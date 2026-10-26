//go:build linux || darwin

// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package sztpclient

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fl-sztp-client/internal/logging"

	"github.com/github/smimesign/ietf-cms/protocol"
)

type httpLimitCase struct {
	name, body string
	request    func(*Client, context.Context) error
}

func TestNormalizeRedirectAddress(t *testing.T) {
	for _, tc := range []struct {
		name, address string
		port          int
		want          string
	}{
		{"IPv4", "192.0.2.1", 9090, "https://192.0.2.1:9090"},
		{"bare IPv6", "2001:db8::1", 9090, "https://[2001:db8::1]:9090"},
		{"bracketed IPv6", "[2001:db8::1]", 9090, "https://[2001:db8::1]:9090"},
		{"IPv6 zone", "fe80::1%eth0", 9090, "https://[fe80::1%25eth0]:9090"},
		{"hostname", "bootstrap.example", 9090, "https://bootstrap.example:9090"},
		{"Unicode hostname", "bücher.example", 9090, "https://xn--bcher-kva.example:9090"},
		{"HTTP upgrade", "http://bootstrap.example", 9090, "https://bootstrap.example:9090"},
		{"HTTPS URL", "https://bootstrap.example", 9090, "https://bootstrap.example:9090"},
		{"IPv6 URL", "https://[2001:db8::1]", 9090, "https://[2001:db8::1]:9090"},
		{"IPv6 zone URL", "https://[fe80::1%25eth0]:443", 9090, "https://[fe80::1%25eth0]:9090"},
		{"replace embedded port", "bootstrap.example:443", 9090, "https://bootstrap.example:9090"},
		{"discard URL components", "https://user:password@bootstrap.example:443/path?query#fragment", 9090, "https://bootstrap.example:9090"},
		{"outer whitespace", " \t2001:db8::1\n", 9090, "https://[2001:db8::1]:9090"},
		{"lowest port", "bootstrap.example", 1, "https://bootstrap.example:1"},
		{"highest port", "bootstrap.example", 65535, "https://bootstrap.example:65535"},
		{"empty address", "", 9090, ""},
		{"whitespace address", " \t\n", 9090, ""},
		{"invalid hostname", "bootstrap..example", 9090, ""},
		{"invalid IPv6", "2001:db8::zz", 9090, ""},
		{"unsupported scheme", "ftp://bootstrap.example", 9090, ""},
		{"invalid embedded port", "bootstrap.example:65536", 9090, ""},
		{"negative port", "bootstrap.example", -1, ""},
		{"zero port", "bootstrap.example", 0, ""},
		{"oversized port", "bootstrap.example", 65536, ""},
		{"oversized IPv6 port", "2001:db8::1", 65536, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeRedirectAddress(tc.address, tc.port)
			if tc.want == "" {
				if err == nil || got != "" {
					t.Fatalf("normalization = %q, %v; want an error and no URL", got, err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("normalization = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestBootstrapRedirectRejectsInvalidTarget(t *testing.T) {
	for _, entry := range []string{
		`{"address":"bootstrap.example"}`,
		`{"address":"bootstrap.example","port":-1}`,
		`{"address":"bootstrap.example","port":0}`,
		`{"address":"bootstrap.example","port":65536}`,
		`{"address":"2001:db8::1","port":65536}`,
		`{"address":"bootstrap..example","port":9090}`,
		`{"address":"","port":9090}`,
	} {
		t.Run(entry, func(t *testing.T) {
			const original = "https://original.example:8443"
			client := NewClient(slog.New(slog.NewTextHandler(io.Discard, nil)), false, original, "", "", "", "", "", "")
			payload := `{"ietf-sztp-conveyed-info:redirect-information":{"bootstrap-server":[` + entry + `]}}`
			if err := json.Unmarshal([]byte(payload), &client.BootstrapServerRedirectInfo); err != nil {
				t.Fatal(err)
			}
			// A canceled context also prevents any request if validation regresses.
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			err := client.getBootstrapRedirect(ctx)
			if err == nil || !strings.Contains(err.Error(), "invalid bootstrap redirect") || errors.Is(err, context.Canceled) {
				t.Fatalf("redirect error = %v, want target validation before a request", err)
			}
			if client.BootstrapServerURL != original {
				t.Fatalf("invalid target changed server to %q", client.BootstrapServerURL)
			}
		})
	}
}

func httpLimitCases() []httpLimitCase {
	return []httpLimitCase{
		{"host-meta", `<XRD xmlns="http://docs.oasis-open.org/ns/xri/xrd-1.0"/>`, func(c *Client, ctx context.Context) error { _, err := c.getHostMeta(ctx); return err }},
		{"bootstrap", `{}`, func(c *Client, ctx context.Context) error {
			_, err := c.postRequestWithContext(ctx, "{}", c.BootstrapServerURL, false, http.StatusOK)
			return err
		}},
		{"progress", `{}`, func(c *Client, ctx context.Context) error {
			_, err := c.postRequestWithContext(ctx, "{}", c.BootstrapServerURL, true, http.StatusOK)
			return err
		}},
	}
}

func TestHTTPRequestLimitsDeadlines(t *testing.T) {
	for _, request := range httpLimitCases() {
		for _, started := range []bool{false, true} {
			for _, parent := range []string{"background", "earlier deadline", "canceled"} {
				t.Run(fmt.Sprintf("%s/body-started=%t/%s", request.name, started, parent), func(t *testing.T) {
					ctx := context.Background()
					cancel := func() {}
					want := context.DeadlineExceeded
					if parent == "earlier deadline" {
						ctx, cancel = context.WithTimeout(ctx, 50*time.Millisecond)
					}
					if parent == "canceled" {
						ctx, cancel = context.WithCancel(ctx)
						want = context.Canceled
					}
					defer cancel()
					release := make(chan struct{})
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						_, _ = io.Copy(io.Discard, r.Body)
						if started {
							_, _ = io.WriteString(w, "partial")
							w.(http.Flusher).Flush()
						}
						if parent == "canceled" {
							cancel()
						}
						select {
						case <-r.Context().Done():
						case <-release:
						}
					}))
					defer func() { close(release); server.Close() }()
					client := NewClient(slog.New(slog.NewTextHandler(io.Discard, nil)), false, server.URL, "", "", "", "", "", "")
					timeout := 200 * time.Millisecond
					if parent != "background" {
						timeout = 10 * time.Second
					}
					client.RequestLimits = HTTPRequestLimits{HostMetaTimeout: timeout, BootstrapTimeout: timeout, ProgressTimeout: timeout}
					result := make(chan error, 1)
					go func() { result <- request.request(client, ctx) }()
					select {
					case err := <-result:
						if !errors.Is(err, want) {
							t.Fatalf("error = %v, want %v", err, want)
						}
					case <-time.After(2 * time.Second):
						t.Fatal("request failed to honor its deadline or cancellation")
					}
					if parent == "background" && ctx.Err() != nil {
						t.Fatalf("request canceled its parent: %v", ctx.Err())
					}
				})
			}
		}
	}
}

func TestHTTPResponseSizeLimits(t *testing.T) {
	const limit = 128
	for _, request := range httpLimitCases() {
		for _, encoding := range []string{"length", "chunked", "gzip"} {
			for _, size := range []int{limit - 1, limit, limit + 1} {
				t.Run(fmt.Sprintf("%s/%s/%d", request.name, encoding, size), func(t *testing.T) {
					body := request.body + strings.Repeat(" ", size-len(request.body))
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						switch encoding {
						case "length":
							w.Header().Set("Content-Length", fmt.Sprint(len(body)))
						case "chunked":
							w.(http.Flusher).Flush()
						case "gzip":
							w.Header().Set("Content-Encoding", "gzip")
							z := gzip.NewWriter(w)
							_, _ = io.WriteString(z, body)
							_ = z.Close()
							return
						}
						_, _ = io.WriteString(w, body)
					}))
					defer server.Close()
					client := NewClient(slog.New(slog.NewTextHandler(io.Discard, nil)), false, server.URL, "", "", "", "", "", "")
					// Only the selected profile is restrictive: this catches using
					// the bootstrap cap for host metadata or progress responses.
					switch request.name {
					case "host-meta":
						client.RequestLimits.HostMetaMaxResponseBytes = limit
					case "bootstrap":
						client.RequestLimits.BootstrapMaxResponseBytes = limit
					case "progress":
						client.RequestLimits.ProgressMaxResponseBytes = limit
					}
					err := request.request(client, context.Background())
					if size > limit {
						if !errors.Is(err, ErrResponseTooLarge) {
							t.Fatalf("error = %v, want oversized response", err)
						}
					} else if err != nil {
						t.Fatalf("in-limit response failed: %v", err)
					}
				})
			}
		}
	}
}

func TestHTTPResponseLimitStopsReading(t *testing.T) {
	reader := strings.NewReader(strings.Repeat("secret response ", 1000))
	before := reader.Len()
	data, err := readResponseBody(reader, 64)
	if !errors.Is(err, ErrResponseTooLarge) || data != nil || strings.Contains(err.Error(), "secret") || before-reader.Len() != 65 {
		t.Fatalf("data=%q error=%v bytes read=%d", data, err, before-reader.Len())
	}
}

func TestHTTPRequestLimitsDefaultsAndValidation(t *testing.T) {
	defaults := DefaultHTTPRequestLimits()
	if defaults != (HTTPRequestLimits{HostMetaTimeout: 30 * time.Second, BootstrapTimeout: 120 * time.Second, ProgressTimeout: 30 * time.Second,
		HostMetaMaxResponseBytes: 1 << 20, BootstrapMaxResponseBytes: 16 << 20, ProgressMaxResponseBytes: 64 << 10}) {
		t.Fatalf("unexpected default policy: %+v", defaults)
	}
	if got := (HTTPRequestLimits{}).withDefaults(); got != defaults {
		t.Fatalf("zero limits = %+v", got)
	}
	partial := HTTPRequestLimits{BootstrapTimeout: time.Second}
	want := defaults
	want.BootstrapTimeout = time.Second
	if got := partial.withDefaults(); got != want {
		t.Fatalf("partial limits = %+v", got)
	}
	for _, limits := range []HTTPRequestLimits{
		{HostMetaTimeout: -1}, {BootstrapTimeout: -1}, {ProgressTimeout: -1},
		{HostMetaMaxResponseBytes: -1}, {BootstrapMaxResponseBytes: -1}, {ProgressMaxResponseBytes: -1},
		{HostMetaMaxResponseBytes: int64(^uint(0) >> 1)},
	} {
		if limits.Validate() == nil {
			t.Fatalf("accepted invalid limits: %+v", limits)
		}
	}
}

func TestBootstrapPreservesHTTPErrors(t *testing.T) {
	for _, path := range []string{"/.well-known/host-meta", "/restconf/operations/ietf-sztp-bootstrap-server:get-bootstrapping-data"} {
		for _, failure := range []string{"timeout", "oversize"} {
			t.Run(path+"/"+failure, func(t *testing.T) {
				release := make(chan struct{})
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.Copy(io.Discard, r.Body)
					if r.URL.Path == path {
						if failure == "timeout" {
							select {
							case <-r.Context().Done():
							case <-release:
							}
							return
						}
						_, _ = io.WriteString(w, strings.Repeat("x", 513))
						return
					}
					if r.URL.Path == "/.well-known/host-meta" {
						_, _ = io.WriteString(w, `<XRD xmlns="http://docs.oasis-open.org/ns/xri/xrd-1.0"><Link rel="restconf" href="/restconf"/></XRD>`)
						return
					}
					w.WriteHeader(http.StatusNoContent)
				}))
				defer func() { close(release); server.Close() }()
				client := NewClient(slog.New(slog.NewTextHandler(io.Discard, nil)), false, server.URL, t.TempDir(), "test-device", "", "", "", "")
				client.RequestLimits = HTTPRequestLimits{HostMetaTimeout: 200 * time.Millisecond, BootstrapTimeout: 200 * time.Millisecond, HostMetaMaxResponseBytes: 512, BootstrapMaxResponseBytes: 512}
				_, err := client.RunBootstrapSequence(context.Background())
				want := error(context.DeadlineExceeded)
				if failure == "oversize" {
					want = ErrResponseTooLarge
				}
				if !errors.Is(err, want) {
					t.Fatalf("error chain lost %v: %v", want, err)
				}
			})
		}
	}
}

func onboardingTestFields(config, pre, post string) map[string]string {
	return map[string]string{
		"configuration":             base64.StdEncoding.EncodeToString([]byte(config)),
		"pre-configuration-script":  base64.StdEncoding.EncodeToString([]byte(pre)),
		"post-configuration-script": base64.StdEncoding.EncodeToString([]byte(post)),
	}
}

func TestProgressRequestDeadline(t *testing.T) {
	for _, responseStarted := range []bool{false, true} {
		name := "before headers"
		if responseStarted {
			name = "during response body"
		}
		t.Run(name, func(t *testing.T) {
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if responseStarted {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("partial response"))
					w.(http.Flusher).Flush()
				}
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}))
			defer func() { close(release); server.Close() }()
			logger := logging.New(io.Discard, false)
			client := NewClient(logger, false, server.URL, t.TempDir(), "test-device", "", "", "", "")
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			started := time.Now()
			_, err := client.postRequestWithContext(ctx, "{}", server.URL, true, http.StatusNoContent)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("request error=%v, want deadline exceeded", err)
			}
			if elapsed := time.Since(started); elapsed > 2*time.Second {
				t.Fatalf("request exceeded its deadline: %s", elapsed)
			}
		})
	}
}

func TestRequestErrorsAreReturnedWithoutDuplicateLogs(t *testing.T) {
	for _, kind := range []string{"transport", "body", "decode"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch kind {
				case "transport":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = conn.Close()
				case "body":
					w.Header().Set("Content-Length", "100")
					_, _ = w.Write([]byte("short"))
				case "decode":
					_, _ = w.Write([]byte("not JSON"))
				}
			}))
			defer server.Close()
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
			client := NewClient(logger, false, server.URL, "", "test-device", "", "", "", "")
			_, err := client.postRequest("{}", server.URL, false, http.StatusOK)
			if err == nil {
				t.Fatal("request unexpectedly succeeded")
			}
			if kind == "body" && !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("lost read failure: %v", err)
			}
			if kind == "decode" && !strings.Contains(err.Error(), "not JSON") {
				t.Fatalf("lost response context: %v", err)
			}
			if logs.Len() != 0 {
				t.Fatalf("returned error was also logged: %s", &logs)
			}
		})
	}
}

func TestBestEffortProgressFailureLogsOnce(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	var logs bytes.Buffer
	client := NewClient(slog.New(slog.NewJSONHandler(&logs, nil)), false, server.URL, "", "test-device", "", "", "", "")
	client.postProgress(context.Background(), ProgressTypePreScriptInitiated, "Report starting")
	var event map[string]any
	if err := json.Unmarshal(logs.Bytes(), &event); err != nil {
		t.Fatalf("expected one warning: %v; logs=%s", err, &logs)
	}
	if event["level"] != "WARN" || event["progress_type"] != ProgressTypePreScriptInitiated.String() || event["error"] == nil {
		t.Fatalf("missing progress failure context: %v", event)
	}
}

func TestScriptFailureRetainsPhaseAndReportsWithoutDuplicateLogs(t *testing.T) {
	for _, phase := range []string{PRE, POST} {
		t.Run(phase, func(t *testing.T) {
			pre, post := "", ""
			if phase == PRE {
				pre = "exit 7\n"
			} else {
				post = "exit 7\n"
			}
			client, logs, reports := onboardingTestClient(t, t.TempDir(), onboardingTestFields("", pre, post), nil)
			client.Logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
			_, err := client.RunBootstrapSequence(context.Background())
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 || !strings.Contains(err.Error(), "run "+phase+" configuration script") {
				t.Fatalf("lost phase or underlying exit error: %v", err)
			}
			for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte{'\n'}) {
				if len(line) == 0 {
					continue
				}
				var event map[string]any
				if err := json.Unmarshal(line, &event); err != nil {
					t.Fatal(err)
				}
				if event["error"] != nil || event["level"] == "ERROR" {
					t.Fatalf("returned script failure was also logged: %v", event)
				}
			}
			want := map[string]string{
				phase + "-script-error": "Error running the " + phase + "-configuration script: exit status 7",
				"bootstrap-error":       "Error launching " + phase + " scripts: exit status 7",
			}
			for len(reports) > 0 {
				input := (<-reports).IetfSztpBootstrapServerInput
				if message, ok := want[input.ProgressType]; ok {
					if input.Message != message {
						t.Errorf("report %s = %q, want %q", input.ProgressType, input.Message, message)
					}
					delete(want, input.ProgressType)
				}
			}
			if len(want) != 0 {
				t.Fatalf("missing existing error reports: %v", want)
			}
		})
	}
}

// Exercise the public bootstrap sequence with an in-process server, so tests
// cover preparation and execution ordering rather than invoking them manually.
func onboardingTestClient(t *testing.T, tmp string, fields map[string]string, onProgress func(ProgressJSON)) (*Client, *bytes.Buffer, <-chan ProgressJSON) {
	t.Helper()
	info, err := json.Marshal(map[string]any{
		"ietf-sztp-conveyed-info:onboarding-information": fields,
	})
	if err != nil {
		t.Fatal(err)
	}
	octets, err := asn1.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	conveyed, err := asn1.Marshal(protocol.ContentInfo{
		ContentType: asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1},
		Content:     asn1.RawValue{Class: 2, Tag: 0, IsCompound: true, Bytes: octets},
	})
	if err != nil {
		t.Fatal(err)
	}
	var response BootstrapServerPostOutput
	response.IetfSztpBootstrapServerOutput.ConveyedInformation = base64.StdEncoding.EncodeToString(conveyed)
	reports := make(chan ProgressJSON, 128)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/host-meta":
			_, _ = w.Write([]byte("<XRD xmlns=\"http://docs.oasis-open.org/ns/xri/xrd-1.0\"><Link rel=\"restconf\" href=\"/restconf\"/></XRD>"))
		case "/restconf/operations/ietf-sztp-bootstrap-server:get-bootstrapping-data":
			_ = json.NewEncoder(w).Encode(response)
		case "/restconf/operations/ietf-sztp-bootstrap-server:report-progress":
			var progress ProgressJSON
			if err := json.NewDecoder(r.Body).Decode(&progress); err != nil {
				t.Errorf("decode progress: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if onProgress != nil {
				onProgress(progress)
			}
			reports <- progress
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	var logs bytes.Buffer
	logger := logging.New(&logs, true)
	client := NewClient(logger, true, server.URL, tmp, "test-device", "", "", "", "")
	return client, &logs, reports
}

func onboardingTestDirs(t *testing.T, parent string) []string {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "onboarding-") {
			dirs = append(dirs, filepath.Join(parent, entry.Name()))
		}
	}
	return dirs
}

func onboardingTestRead(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != want {
		t.Fatalf("read %q = %q (%v), want %q", path, data, err, want)
	}
}

func TestBootstrapOnboardingWorkingDirectory(t *testing.T) {
	for _, tc := range []struct {
		name     string
		bash     bool
		relative bool
	}{
		{name: "bash absolute", bash: true},
		{name: "bash relative", bash: true, relative: true},
		{name: "sh absolute"},
		{name: "sh relative", relative: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := t.TempDir()
			if tc.bash {
				bash, err := exec.LookPath("bash")
				if err != nil {
					t.Skip("bash is unavailable")
				}
				if err := os.Symlink(bash, filepath.Join(bin, "bash")); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink("/bin/cat", filepath.Join(bin, "cat")); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)
			root := t.TempDir()
			tmp := filepath.Join(root, "bootstrap files")
			if err := os.Mkdir(tmp, 0755); err != nil {
				t.Fatal(err)
			}
			if tc.relative {
				t.Chdir(root)
				tmp = "bootstrap files"
			}
			parent, err := filepath.Abs(tmp)
			if err != nil {
				t.Fatal(err)
			}
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			pre := "set -eu\n" +
				"test -f ./post-configuration.sh\n" +
				"cat ./configuration > ./shared\n" +
				"printf 'pre\\n' > ./script-runs\n" +
				"printf '%s\\n' \"$PWD\" > ./pre-cwd\n" +
				"cd /\n"
			post := "set -eu\n" +
				"test -f ./pre-configuration.sh\n" +
				"test \"$(cat ./configuration)\" = \"$(cat ./shared)\"\n" +
				"printf 'post\\n' >> ./script-runs\n" +
				"printf '%s\\n' \"$PWD\" > ./post-cwd\n"
			client, logs, _ := onboardingTestClient(t, tmp, onboardingTestFields("device configuration\n", pre, post), nil)
			client.Logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			if _, err := client.RunBootstrapSequence(context.Background()); err != nil {
				t.Fatalf("bootstrap: %v\n%s", err, logs)
			}
			dirs := onboardingTestDirs(t, parent)
			if len(dirs) != 1 {
				t.Fatalf("onboarding folders = %v, want one", dirs)
			}
			dir := dirs[0]
			onboardingTestRead(t, filepath.Join(dir, "configuration"), "device configuration\n")
			onboardingTestRead(t, filepath.Join(dir, "pre-configuration.sh"), pre)
			onboardingTestRead(t, filepath.Join(dir, "post-configuration.sh"), post)
			onboardingTestRead(t, filepath.Join(dir, "shared"), "device configuration\n")
			onboardingTestRead(t, filepath.Join(dir, "script-runs"), "pre\npost\n")
			onboardingTestRead(t, filepath.Join(dir, "pre-cwd"), dir+"\n")
			onboardingTestRead(t, filepath.Join(dir, "post-cwd"), dir+"\n")
			after, err := os.Getwd()
			if err != nil || after != cwd {
				t.Fatalf("parent working directory = %q (%v), want %q", after, err, cwd)
			}
			if client.TmpFolder != tmp {
				t.Fatalf("temporary parent changed: %q, want %q", client.TmpFolder, tmp)
			}
			stamp, suffix, ok := strings.Cut(strings.TrimPrefix(filepath.Base(dir), "onboarding-"), "-")
			if _, err := time.Parse("20060102T150405Z", stamp); err != nil || !ok || suffix == "" {
				t.Fatalf("invalid onboarding folder name %q: %v", dir, err)
			}
			for name, mode := range map[string]os.FileMode{
				"": 0700, "configuration": 0744, "pre-configuration.sh": 0755, "post-configuration.sh": 0755,
			} {
				info, err := os.Stat(filepath.Join(dir, name))
				if err != nil || info.Mode().Perm() != mode {
					t.Fatalf("permissions for %q: %v, want %o", name, info, mode)
				}
			}
			wantShell := "sh"
			if tc.bash {
				wantShell = filepath.Join(bin, "bash")
			}
			shells := make(map[string]int)
			decoder := json.NewDecoder(logs)
			for decoder.More() {
				var record map[string]any
				if err := decoder.Decode(&record); err != nil {
					t.Fatal(err)
				}
				if shell, ok := record["shell"]; ok {
					if shell != wantShell {
						t.Fatalf("unexpected shell: %v, want %s", shell, wantShell)
					}
					shells[record["phase"].(string)]++
				}
			}
			if len(shells) != 2 || shells[PRE] != 1 || shells[POST] != 1 {
				t.Fatalf("expected both scripts to use selected shell: %v", shells)
			}
		})
	}
}

func TestBootstrapLeavesArtifactsForCallerToPersistAndClean(t *testing.T) {
	parent := t.TempDir()
	pre := "set -eu\nprintf 'pre\\n' >> ./script-runs\ntest -f ../allow-success\n"
	post := "printf 'post\\n' >> ./script-runs\n"
	client, logs, _ := onboardingTestClient(t, parent, onboardingTestFields("original configuration\n", pre, post), nil)
	if _, err := client.RunBootstrapSequence(context.Background()); err == nil {
		t.Fatal("first attempt unexpectedly succeeded")
	}
	dirs := onboardingTestDirs(t, parent)
	if len(dirs) != 1 {
		t.Fatalf("first attempt folders = %v", dirs)
	}
	first := dirs[0]
	onboardingTestRead(t, filepath.Join(first, "script-runs"), "pre\n")
	if err := os.WriteFile(filepath.Join(parent, "allow-success"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := client.RunBootstrapSequence(context.Background()); err != nil {
		t.Fatalf("retry: %v\n%s", err, logs)
	}
	dirs = onboardingTestDirs(t, parent)
	if len(dirs) != 2 {
		t.Fatalf("retry folders = %v, want two", dirs)
	}
	for _, dir := range dirs {
		onboardingTestRead(t, filepath.Join(dir, "configuration"), "original configuration\n")
		onboardingTestRead(t, filepath.Join(dir, "pre-configuration.sh"), pre)
		onboardingTestRead(t, filepath.Join(dir, "post-configuration.sh"), post)
		want := "pre\npost\n"
		if dir == first {
			want = "pre\n"
		}
		onboardingTestRead(t, filepath.Join(dir, "script-runs"), want)
	}
}

func TestBootstrapOnboardingPreparationFailureStopsScripts(t *testing.T) {
	for _, field := range []string{"configuration", "pre-configuration-script", "post-configuration-script", "folder", "write"} {
		t.Run(field, func(t *testing.T) {
			parent := t.TempDir()
			tmp := parent
			fields := onboardingTestFields("configuration\n", "printf pre > ./script-runs\n", "printf post >> ./script-runs\n")
			var onProgress func(ProgressJSON)
			wantError := "decode onboarding file"
			switch field {
			case "folder":
				tmp = filepath.Join(parent, "missing")
				wantError = "create onboarding folder"
			case "write":
				wantError = "prepare onboarding file"
				// Make the last file unwritable after the attempt folder exists.
				onProgress = func(report ProgressJSON) {
					if report.IetfSztpBootstrapServerInput.ProgressType == "config-initiated" {
						for _, dir := range onboardingTestDirs(t, parent) {
							if err := os.Mkdir(filepath.Join(dir, "post-configuration.sh"), 0700); err != nil {
								t.Errorf("make conflicting directory: %v", err)
							}
						}
					}
				}
			default:
				fields[field] = "%%%"
			}
			client, logs, reports := onboardingTestClient(t, tmp, fields, onProgress)
			_, err := client.RunBootstrapSequence(context.Background())
			if err == nil || !strings.Contains(err.Error(), wantError) {
				t.Fatalf("bootstrap error = %v, want %q\n%s", err, wantError, logs)
			}
			for _, dir := range onboardingTestDirs(t, parent) {
				if _, err := os.Stat(filepath.Join(dir, "script-runs")); !os.IsNotExist(err) {
					t.Fatalf("scripts ran after preparation failed: %v", err)
				}
				if field != "write" {
					entries, err := os.ReadDir(dir)
					if err != nil || len(entries) != 0 {
						t.Fatalf("malformed data produced files: %v (%v)", entries, err)
					}
				}
			}
			var bootstrapError bool
			for len(reports) > 0 {
				progress := (<-reports).IetfSztpBootstrapServerInput.ProgressType
				switch progress {
				case "bootstrap-error":
					bootstrapError = true
				case "pre-script-initiated", "post-script-initiated", "bootstrap-complete":
					t.Errorf("unexpected progress after preparation failure: %s", progress)
				}
			}
			if !bootstrapError {
				t.Fatal("preparation failure did not report bootstrap-error")
			}
		})
	}
}

func TestBootstrapOnboardingEmptyArtifacts(t *testing.T) {
	client, logs, _ := onboardingTestClient(t, t.TempDir(), map[string]string{}, nil)
	if _, err := client.RunBootstrapSequence(context.Background()); err != nil {
		t.Fatalf("empty bootstrap: %v\n%s", err, logs)
	}
	dirs := onboardingTestDirs(t, client.TmpFolder)
	if len(dirs) != 1 {
		t.Fatalf("onboarding folders = %v, want one", dirs)
	}
	entries, err := os.ReadDir(dirs[0])
	if err != nil || len(entries) != 3 {
		t.Fatalf("empty artifacts = %v (%v), want three files", entries, err)
	}
	for _, entry := range entries {
		onboardingTestRead(t, filepath.Join(dirs[0], entry.Name()), "")
	}
}
