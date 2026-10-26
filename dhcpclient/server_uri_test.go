// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package dhcpclient

import (
	"bytes"
	"strings"
	"testing"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv6"
)

func TestServerFromOptionURIList(t *testing.T) {
	const origin = "https://bootstrap:8080"
	const single = "\x00\x16" + origin
	longURI := origin + "/" + strings.Repeat("a", 277)

	tests := []struct {
		name string
		data string
		want string
	}{
		{"single URI", single, origin},
		{"first valid URI wins", single + "\x00\x12https://other:8081", origin},
		{"invalid then valid", "\x00\x08https://" + single, origin},
		{"invalid hostname then valid", "\x00\x1ehttps://bootstrap..example:443" + single, origin},
		{"only invalid hostname", "\x00\x1ehttps://bootstrap..example:443", ""},
		{"empty then valid", "\x00\x00" + single, origin},
		{"unsupported scheme then valid", "\x00\x14ftp://bootstrap:8080" + single, origin},
		{"discard URL suffix", "\x00\x21https://bootstrap:8080/path?x=1#f", origin},
		{"HTTP compatibility", "\x00\x15http://bootstrap:8080", origin},
		{"bare address compatibility", "\x00\x0ebootstrap:8080", origin},
		{"IPv6 address", "\x00\x1ahttps://[2001:db8::1]:8080", "https://[2001:db8::1]:8080"},
		{"URI longer than 255 bytes", "\x01\x2c" + longURI, origin},
		{"length counts bytes", "\x00\x19" + origin + "/ä", origin},
		{"Unicode hostname with underscore", "\x00\x23https://bücher_server.example:8443", "https://xn--bcher_server-dlb.example:8443"},
		{"Unicode beside ASCII consecutive hyphens", "\x00\x23https://ab--cd.bücher.example:8443", "https://ab--cd.xn--bcher-kva.example:8443"},
		{"empty option", "", ""},
		{"empty URI", "\x00\x00", ""},
		{"no usable URI", "\x00\x08https://", ""},
		{"truncated length", "\x00", ""},
		{"truncated URI", "\x00\x16https://bootstrap", ""},
		{"trailing byte", single + "\x00", ""},
		{"truncated later URI", single + "\x00\x05abc", ""},
		{"one byte length", "\x16" + origin, ""},
		{"little endian length", "\x16\x00" + origin, ""},
		{"maximum length without payload", "\xff\xff", ""},
		{"invalid UTF-8 textual fallback", "\xff", ""},
		{"invalid UTF-8 URI then valid", "\x00\x02\xff\xff" + single, origin},
	}
	for _, code := range []int{143, 136} {
		for _, tt := range tests {
			t.Run(fmtOptionName(code, tt.name), func(t *testing.T) {
				logger, output := newOptionLogger()
				got := serverFromOption(logger, "eth-test", code, []byte(tt.data), true)
				if got != tt.want {
					t.Fatalf("got %q, want %q", got, tt.want)
				}
				entry := readOptionLog(t, output)
				if entry.Interface != "eth-test" || entry.Option != code {
					t.Errorf("missing interface or option fields: %v", entry)
				}
				if tt.want == "" {
					if entry.Level != "WARN" || entry.Error == "" {
						t.Errorf("invalid option lacks warning or error: %v", entry)
					}
					if strings.Contains(entry.Message, "not found") {
						t.Errorf("present option reported as missing: %s", entry.Message)
					}
				} else if entry.Level != "INFO" || entry.Server != tt.want {
					t.Errorf("success log lacks canonical origin: %v", entry)
				}
			})
		}
	}
}

func TestServerFromSplitDHCP4Option(t *testing.T) {
	const origin = "https://bootstrap:8080"
	uri := origin + "/" + strings.Repeat("a", 277)
	payload := []byte("\x01\x2c" + uri)

	// Split inside the URI length field and across the URI payload.
	raw := append([]byte{143, 1}, payload[:1]...)
	raw = append(raw, 143, 255)
	raw = append(raw, payload[1:256]...)
	raw = append(raw, 143, byte(len(payload)-256))
	raw = append(raw, payload[256:]...)
	raw = append(raw, 255)

	options := make(dhcpv4.Options)
	if err := options.FromBytes(raw); err != nil {
		t.Fatal(err)
	}
	offer := &dhcpv4.DHCPv4{Options: options}
	code := dhcpv4.GenericOptionCode(143)
	data := offer.GetOneOption(code)
	if !bytes.Equal(data, payload) {
		t.Fatal("DHCPv4 option fragments were not reassembled")
	}
	logger, _ := newOptionLogger()
	got := serverFromOption(logger, "eth-test", 143, data, offer.Options.Has(code))
	if got != origin {
		t.Fatalf("got %q, want %q", got, origin)
	}
}

func TestServerFromDHCP6OptionPayload(t *testing.T) {
	const origin = "https://bootstrap:8080"
	payload := []byte("\x00\x16" + origin)

	// The outer option length includes the two-byte URI length field.
	raw := append([]byte{0, 136, 0, 24}, payload...)
	var options dhcpv6.Options
	if err := options.FromBytes(raw); err != nil {
		t.Fatal(err)
	}
	option := options.GetOne(dhcpv6.OptionCode(136))
	if option == nil {
		t.Fatal("DHCPv6 option 136 is missing")
	}
	if !bytes.Equal(option.ToBytes(), payload) {
		t.Fatal("DHCPv6 option payload includes an outer header")
	}
	logger, _ := newOptionLogger()
	got := serverFromOption(logger, "eth-test", 136, option.ToBytes(), true)
	if got != origin {
		t.Fatalf("got %q, want %q", got, origin)
	}
}
