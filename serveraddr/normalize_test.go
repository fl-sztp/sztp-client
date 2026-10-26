// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package serveraddr

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNormalizeHostnameValidation(t *testing.T) {
	maxLabel := strings.Repeat("a", 63)
	maxName := strings.Repeat(maxLabel+".", 3) + strings.Repeat("a", 61)
	tests := []struct {
		name, raw, want string
	}{
		{"single label", "b", "https://b"},
		{"case and port spelling", "Bootstrap.EXAMPLE:00443", "https://Bootstrap.EXAMPLE:00443"},
		{"underscore", "bootstrap_server:8443", "https://bootstrap_server:8443"},
		{"underscore at label edges", "_bootstrap_.example", "https://_bootstrap_.example"},
		{"internal consecutive hyphens", "ab--cd.example", "https://ab--cd.example"},
		{"trailing root dot", "bootstrap.example.:8443", "https://bootstrap.example.:8443"},
		{"Unicode hostname", "https://bücher.example:8443", "https://xn--bcher-kva.example:8443"},
		{"Unicode with underscore", "bücher_server.example", "https://xn--bcher_server-dlb.example"},
		{"Unicode with consecutive hyphens", "bü--cher.example", "https://xn--b--cher-n2a.example"},
		{"Unicode beside ASCII consecutive hyphens", "ab--cd.bücher.example", "https://ab--cd.xn--bcher-kva.example"},
		{"Unicode label separator", "bücher。example", "https://xn--bcher-kva.example"},
		{"Unicode with root dot and port spelling", "bücher_server.example.:00443", "https://xn--bcher_server-dlb.example.:00443"},
		{"punycode hostname", "xn--bcher-kva.example", "https://xn--bcher-kva.example"},
		{"maximum label length", maxLabel, "https://" + maxLabel},
		{"maximum hostname length", maxName, "https://" + maxName},
		{"maximum hostname with root dot", maxName + ".", "https://" + maxName + "."},
		{"maximum Unicode label after encoding", strings.Repeat("ü", 57) + ".example", "https://xn--td" + strings.Repeat("a", 57) + ".example"},
		{"maximum Unicode hostname after encoding", strings.Repeat("ü", 57) + maxName[63:], "https://xn--td" + strings.Repeat("a", 57) + maxName[63:]},
		{"empty interior label", "https://bootstrap..example:443", ""},
		{"empty leading label", ".bootstrap.example", ""},
		{"two trailing dots", "bootstrap.example..", ""},
		{"root alone", ".", ""},
		{"leading hyphen", "-bootstrap.example", ""},
		{"trailing hyphen", "bootstrap-.example", ""},
		{"punctuation", "bootstrap!.example", ""},
		{"comma", "bootstrap,example", ""},
		{"oversized label", strings.Repeat("a", 64) + ".example", ""},
		{"oversized hostname", maxName + "a", ""},
		{"oversized hostname with root dot", maxName + "a.", ""},
		{"oversized Unicode label after encoding", strings.Repeat("ü", 58) + ".example", ""},
		{"oversized Unicode hostname after encoding", strings.Repeat("ü", 57) + maxName[63:] + "a", ""},
		{"Unicode empty label", "bücher。。example", ""},
		{"Unicode leading hyphen", "-bücher.example", ""},
		{"Unicode trailing hyphen", "bücher-.example", ""},
		{"Unicode mapping exposes leading hyphen", "\u00ad-bücher.example", ""},
		{"invalid Unicode joiner", "ab\u200dcd.example", ""},
		{"invalid UTF-8 hostname", "ab\xffcd.example", ""},
		{"ASCII-only punycode beside Unicode", "xn--example-.bücher.example", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Normalize(tt.raw)
			if tt.want == "" {
				if err == nil || !strings.Contains(err.Error(), "invalid bootstrap server hostname") {
					t.Fatalf("Normalize(%q) = %q, %v; want a hostname validation error", tt.raw, got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("Normalize(%q) = %q, %v; want %q", tt.raw, got, err, tt.want)
			}
		})
	}
}

func TestNormalizeHTTPTransportHostname(t *testing.T) {
	for _, tt := range []struct {
		name, raw, wantAddress string
	}{
		{"Unicode", "https://bücher.example:8443", "xn--bcher-kva.example:8443"},
		{"Unicode with underscore", "https://bücher_server.example:8443", "xn--bcher_server-dlb.example:8443"},
		{"Unicode beside ASCII consecutive hyphens", "https://ab--cd.bücher.example:8443", "ab--cd.xn--bcher-kva.example:8443"},
		{"Unicode separator and default port", "bücher。example", "xn--bcher-kva.example:443"},
		{"Unicode root dot and port spelling", "bücher_server.example.:00443", "xn--bcher_server-dlb.example.:00443"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			origin, err := Normalize(tt.raw)
			if err != nil {
				t.Fatal(err)
			}
			// Exercise the HTTP transport's address conversion without DNS or TLS.
			address := make(chan string, 1)
			stopDial := errors.New("stop before connecting")
			transport := &http.Transport{
				DialContext: func(_ context.Context, _ string, addr string) (net.Conn, error) {
					address <- addr
					return nil, stopDial
				},
			}
			t.Cleanup(transport.CloseIdleConnections)
			client := &http.Client{Transport: transport, Timeout: time.Second}
			response, err := client.Get(origin + "/.well-known/host-meta")
			if response != nil {
				response.Body.Close()
			}
			if !errors.Is(err, stopDial) {
				t.Fatalf("request error = %v, want the dial sentinel", err)
			}
			select {
			case got := <-address:
				if got != tt.wantAddress {
					t.Fatalf("transport dial address = %q, want %q", got, tt.wantAddress)
				}
			default:
				t.Fatal("HTTP transport did not dial")
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"reported operation URL", "https://bootstrap:8080/restconf/operations/ietf-sztp-bootstrap-server:get-bootstrapping-data", "https://bootstrap:8080"},
		{"bare name", "bootstrap", "https://bootstrap"},
		{"bare name and port", "bootstrap:8443", "https://bootstrap:8443"},
		{"IPv4", "192.0.2.1", "https://192.0.2.1"},
		{"IPv4 and port", "192.0.2.1:443", "https://192.0.2.1:443"},
		{"IPv6", "[2001:db8::1]", "https://[2001:db8::1]"},
		{"IPv6 and port", "[2001:db8::1]:8443", "https://[2001:db8::1]:8443"},
		{"IPv6 URL", "https://[2001:db8::1]/path", "https://[2001:db8::1]"},
		{"IPv6 zone", "[fe80::1%eth0]:8443", "https://[fe80::1%25eth0]:8443"},
		{"IPv6 URL zone", "https://[fe80::1%25eth0]:8443/path", "https://[fe80::1%25eth0]:8443"},
		{"HTTP upgrade", "http://bootstrap:80/path", "https://bootstrap:80"},
		{"HTTP default", "http://bootstrap/path", "https://bootstrap"},
		{"outer whitespace", " \t https://bootstrap:8443/path \n", "https://bootstrap:8443"},
		{"userinfo", "https://user:password@bootstrap:8443/path", "https://bootstrap:8443"},
		{"query and fragment", "https://bootstrap/path?server=other#fragment", "https://bootstrap"},
		{"scheme relative", "//bootstrap:8080/path?query#fragment", "https://bootstrap:8080"},
		{"scheme relative userinfo", "//user:password@bootstrap/path", "https://bootstrap"},
		{"no DNS resolution", "unresolvable.invalid", "https://unresolvable.invalid"},
		{"lowest port", "bootstrap:1", "https://bootstrap:1"},
		{"highest port", "bootstrap:65535", "https://bootstrap:65535"},
		{"port spelling preserved", "bootstrap:00443", "https://bootstrap:00443"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Normalize(tt.raw)
			if err != nil {
				t.Fatalf("Normalize(%q) returned error: %v", tt.raw, err)
			}
			if got != tt.want {
				t.Errorf("Normalize(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestNormalizeRejectsInvalidAddresses(t *testing.T) {
	inputs := []string{
		"", " \t\n", "https://", "http:///path", "//", "//:443", ":443",
		"ftp://bootstrap", "mailto:user@example.com", "https:bootstrap", "https:///bootstrap",
		"bootstrap:", "https://bootstrap:", "//bootstrap:", "[::1]:",
		"bootstrap:abc", "https://bootstrap:abc", "bootstrap:0", "bootstrap:65536",
		"bootstrap:-1", "bootstrap:+443", "bootstrap:99999999999999999999999999999999",
		"2001:db8::1", "https://2001:db8::1", "::1", "[bootstrap]", "[192.0.2.1]",
		"[2001:db8::zz]", "[2001:db8::1", "2001:db8::1]", "[::1]extra", "[::1%]",
		"https://[fe80::1%25]", "https://bootstrap/%zz", "https://boot strap",
		"user@bootstrap", "user:password@bootstrap", "https://user@", "https://bootstrap:443:80",
		"ftp://bootstrap/path?next=https://other", "user@bootstrap/path/http://other",
		"user:password@bootstrap?next=https://other", "user@bootstrap#https://other",
	}
	for _, raw := range inputs {
		t.Run(raw, func(t *testing.T) {
			if got, err := Normalize(raw); err == nil {
				t.Errorf("Normalize(%q) = %q, want an error", raw, got)
			}
		})
	}
}

func TestNormalizeIgnoresURLsInDiscardedComponents(t *testing.T) {
	authorities := []string{"bootstrap", "bootstrap:8443", "[::1]", "[::1]:8443"}
	suffixes := []struct {
		name string
		raw  string
	}{
		{"path", "/path/http://other"},
		{"query", "?next=https://other"},
		{"fragment", "#https://other"},
	}
	for _, authority := range authorities {
		for _, suffix := range suffixes {
			t.Run(authority+"/"+suffix.name, func(t *testing.T) {
				raw := authority + suffix.raw
				got, err := Normalize(raw)
				if err != nil {
					t.Fatalf("Normalize(%q) returned error: %v", raw, err)
				}
				if want := "https://" + authority; got != want {
					t.Errorf("Normalize(%q) = %q, want %q", raw, got, want)
				}
			})
		}
	}
}
