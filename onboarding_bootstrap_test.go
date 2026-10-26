//go:build linux || darwin

// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fl-sztp-client/sztpclient"
	"github.com/github/smimesign/ietf-cms/protocol"
)

func TestBootstrapRetryLogsFailureOnce(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	workspace, err := createWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := &Config{Server: server.URL, TmpFolder: workspace, DeviceSerialNumber: "override"}
	stop := errors.New("stop after the first attempt")
	_, err = runBootstrapWithRetry(context.Background(), logger, config, func(_ context.Context, delay time.Duration) error {
		if delay != time.Minute {
			t.Fatalf("retry delay changed: %s", delay)
		}
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatalf("unexpected retry result: %v", err)
	}
	decoder := json.NewDecoder(&logs)
	failures, selected := 0, 0
	for decoder.More() {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		if record["event"] == "bootstrap_server_selected" {
			selected++
			if record["server"] != server.URL {
				t.Fatalf("lost selected server: %v", record)
			}
		}
		if record["error"] != nil {
			failures++
			if record["level"] != "WARN" || record["attempt"] != float64(1) || record["delay"] != float64(time.Minute) ||
				!strings.Contains(record["error"].(string), "failed to get host metadata") {
				t.Fatalf("lost retry failure context: %v", record)
			}
		}
	}
	if failures != 1 || selected != 1 {
		t.Fatalf("failure records=%d, server selection records=%d, want one each", failures, selected)
	}
}

func TestBootstrapHTTPFailuresRetryBeforeProvisioning(t *testing.T) {
	for _, stage := range []string{"host-meta", "bootstrap", "redirect"} {
		for _, failure := range []string{"timeout", "oversize"} {
			t.Run(stage+"/"+failure, func(t *testing.T) {
				config := testBootstrapConfig(t)
				config.AllowInsecure = true
				config.DeviceSerialNumber = "test-device"
				config.RequestLimits = sztpclient.HTTPRequestLimits{
					HostMetaTimeout: 200 * time.Millisecond, BootstrapTimeout: 200 * time.Millisecond, ProgressTimeout: time.Second,
					HostMetaMaxResponseBytes: 512, BootstrapMaxResponseBytes: 4096, ProgressMaxResponseBytes: 128,
				}
				scriptLog := filepath.Join(filepath.Dir(config.StateDir), "script-runs")
				t.Setenv("SZTP_M2_SCRIPT_LOG", scriptLog)
				info, err := json.Marshal(map[string]any{"ietf-sztp-conveyed-info:onboarding-information": map[string]string{
					"pre-configuration-script": base64.StdEncoding.EncodeToString([]byte("printf ran >> \"$SZTP_M2_SCRIPT_LOG\"\n")),
				}})
				if err != nil {
					t.Fatal(err)
				}
				good := conveyedResponseFixture(t, info)
				var failed atomic.Bool
				release := make(chan struct{})
				defer close(release)
				fail := func(w http.ResponseWriter, r *http.Request, here string) bool {
					if stage != here || failed.Swap(true) {
						return false
					}
					_, _ = io.Copy(io.Discard, r.Body)
					if failure == "timeout" {
						select {
						case <-r.Context().Done():
						case <-release:
						}
					} else {
						limit := config.RequestLimits.BootstrapMaxResponseBytes
						if stage == "host-meta" {
							limit = config.RequestLimits.HostMetaMaxResponseBytes
						}
						_, _ = io.WriteString(w, strings.Repeat("x", int(limit+1)))
					}
					return true
				}
				target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(r.URL.Path, ":report-progress") {
						w.WriteHeader(http.StatusNoContent)
						return
					}
					if fail(w, r, "redirect") {
						return
					}
					_ = json.NewEncoder(w).Encode(good)
				}))
				defer target.Close()
				host, portText, _ := net.SplitHostPort(strings.TrimPrefix(target.URL, "https://"))
				port, _ := strconv.Atoi(portText)
				redirect := conveyedResponseFixture(t, []byte(fmt.Sprintf(`{"ietf-sztp-conveyed-info:redirect-information":{"bootstrap-server":[{"address":%q,"port":%d}]}}`, host, port)))
				origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/.well-known/host-meta":
						if fail(w, r, "host-meta") {
							return
						}
						_, _ = io.WriteString(w, `<XRD xmlns="http://docs.oasis-open.org/ns/xri/xrd-1.0"><Link rel="restconf" href="/restconf"/></XRD>`)
					case "/restconf/operations/ietf-sztp-bootstrap-server:get-bootstrapping-data":
						if fail(w, r, "bootstrap") {
							return
						}
						if stage == "redirect" {
							_ = json.NewEncoder(w).Encode(redirect)
						} else {
							_ = json.NewEncoder(w).Encode(good)
						}
					case "/restconf/operations/ietf-sztp-bootstrap-server:report-progress":
						w.WriteHeader(http.StatusNoContent)
					default:
						http.NotFound(w, r)
					}
				}))
				defer origin.Close()
				config.Server = origin.URL
				var logs bytes.Buffer
				logger := tmpTestLogger(&logs)
				waits := 0
				wait := func(ctx context.Context, delay time.Duration) error {
					waits++
					if waits != 1 || ctx.Err() != nil || delay != time.Minute {
						t.Fatalf("unexpected retry: count=%d delay=%v parent=%v", waits, delay, ctx.Err())
					}
					if _, err := os.Stat(scriptLog); !os.IsNotExist(err) {
						t.Fatalf("script ran before successful fetch: %v", err)
					}
					if _, err := os.Stat(filepath.Join(config.StateDir, onboardingStateFile)); !os.IsNotExist(err) {
						t.Fatalf("failure marked onboarding complete: %v", err)
					}
					files, err := os.ReadDir(config.TmpFolder)
					if err != nil || len(files) != 0 {
						t.Fatalf("workspace was not cleaned: files=%v error=%v", files, err)
					}
					return nil
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				err = runOnboarding(ctx, logger, config, func(ctx context.Context) (sztpclient.CompletionReport, error) {
					return runBootstrapWithRetry(ctx, logger, config, wait)
				}, func(context.Context, sztpclient.CompletionReport) error { return nil })
				if err != nil || waits != 1 {
					t.Fatalf("onboarding error=%v waits=%d logs=%s", err, waits, &logs)
				}
				runs, err := os.ReadFile(scriptLog)
				if err != nil || string(runs) != "ran" {
					t.Fatalf("script runs=%q error=%v", runs, err)
				}
				if state := readTestState(t, config.StateDir); state.ReportStatus != reportAcknowledged {
					t.Fatalf("state=%+v", state)
				}
			})
		}
	}
}

func TestPendingCompletionUsesCurrentHTTPLimits(t *testing.T) {
	for _, failure := range []string{"timeout", "oversize"} {
		t.Run(failure, func(t *testing.T) {
			config := testBootstrapConfig(t)
			root := filepath.Dir(config.StateDir)
			cert, key, _ := certificateFixture(t, "device", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
			config.DeviceEndEntityCert = writeCertificateFixture(t, root, "client.pem", cert)
			config.DevicePrivateKey = writeCertificateFixture(t, root, "client-key.pem", key)
			config.RequestLimits = sztpclient.HTTPRequestLimits{ProgressTimeout: 200 * time.Millisecond, ProgressMaxResponseBytes: 64}
			var accept atomic.Bool
			release := make(chan struct{})
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if accept.Load() {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if failure == "timeout" {
					select {
					case <-r.Context().Done():
					case <-release:
					}
					return
				}
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(w, strings.Repeat("x", 65))
			}))
			defer func() { close(release); server.Close() }()
			config.BootstrapTrustAnchor = writeCertificateFixture(t, root, "server.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
			state := pendingTestState()
			state.Report.Endpoint = server.URL + "/report"
			store, err := openOnboardingStore(config.StateDir)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Save(state); err != nil {
				_ = store.Close()
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			bootstrap := func(context.Context) (sztpclient.CompletionReport, error) {
				t.Fatal("pending report reran bootstrap")
				return sztpclient.CompletionReport{}, nil
			}
			logger := tmpTestLogger(new(bytes.Buffer))
			send := completionReporter(logger, config)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var reportErr error
			started := time.Now()
			err = runOnboarding(ctx, logger, config, bootstrap, func(ctx context.Context, report sztpclient.CompletionReport) error {
				reportErr = send(ctx, report)
				cancel()
				return reportErr
			})
			want := error(context.DeadlineExceeded)
			if failure == "oversize" {
				want = sztpclient.ErrResponseTooLarge
			}
			if !errors.Is(err, context.Canceled) || !errors.Is(reportErr, want) || time.Since(started) > 2*time.Second {
				t.Fatalf("onboarding=%v report=%v elapsed=%v", err, reportErr, time.Since(started))
			}
			if got := readTestState(t, config.StateDir); got.ReportStatus != reportPending {
				t.Fatalf("failure lost pending state: %+v", got)
			}
			accept.Store(true)
			if err := runOnboarding(context.Background(), logger, config, bootstrap, completionReporter(logger, config)); err != nil {
				t.Fatal(err)
			}
			if got := readTestState(t, config.StateDir); got.ReportStatus != reportAcknowledged {
				t.Fatalf("report was not acknowledged: %+v", got)
			}
		})
	}
}

func TestBootstrapReportsOriginalValidationError(t *testing.T) {
	const responseBody = `{"post_configuration_script":["Ensure this field has no more than 10000 characters."]}`
	const wantMessage = `failed to request bootstrap server onboarding info: json: unknown field "post_configuration_script"; response body: ` + responseBody
	reports := make(chan sztpclient.ProgressJSON, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/host-meta":
			_, _ = w.Write([]byte(`<XRD xmlns="http://docs.oasis-open.org/ns/xri/xrd-1.0"><Link rel="restconf" href="/restconf"/></XRD>`))
		case "/restconf/operations/ietf-sztp-bootstrap-server:get-bootstrapping-data":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(responseBody))
		case "/restconf/operations/ietf-sztp-bootstrap-server:report-progress":
			var progress sztpclient.ProgressJSON
			if err := json.NewDecoder(r.Body).Decode(&progress); err != nil {
				t.Errorf("decode progress report: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			select {
			case reports <- progress:
				w.WriteHeader(http.StatusNoContent)
			default:
				t.Error("unexpected extra progress report")
				w.WriteHeader(http.StatusBadRequest)
			}
		default:
			t.Errorf("unexpected bootstrap request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	trustAnchor := writeCertificateFixture(t, root, "server-ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	var logs bytes.Buffer
	client := sztpclient.NewClient(tmpTestLogger(&logs), false, server.URL, root, "test-device", "", "", "", trustAnchor)
	_, err := client.RunBootstrapSequence(context.Background())
	if err == nil {
		t.Fatal("bootstrap unexpectedly succeeded")
	}
	select {
	case report := <-reports:
		if got := report.IetfSztpBootstrapServerInput.ProgressType; got != "bootstrap-error" {
			t.Errorf("progress type = %q, want bootstrap-error", got)
		}
		if got := report.IetfSztpBootstrapServerInput.Message; got != wantMessage {
			t.Errorf("reported message = %q, want %q", got, wantMessage)
		}
	default:
		t.Fatalf("bootstrap error was not reported: %v\n%s", err, &logs)
	}
	if err.Error() != wantMessage {
		t.Errorf("bootstrap error = %q, want %q", err.Error(), wantMessage)
	}
}

// Exercise real scripts, a bootstrap redirect and mutual TLS. A stopped process
// resumes only its pending report, using the saved endpoint and serial number.
func TestOnboardingResumesFailedFinalReport(t *testing.T) {
	for _, stalled := range []bool{false, true} {
		name := "HTTP error"
		if stalled {
			name = "stalled report"
		}
		t.Run(name, func(t *testing.T) { testOnboardingFinalReport(t, stalled, 0, false) })
	}
	t.Run("configured report deadline", func(t *testing.T) { testOnboardingFinalReport(t, true, 200*time.Millisecond, false) })
}

func TestOnboardingIPv6Redirect(t *testing.T) {
	testOnboardingFinalReport(t, false, 0, true)
}

func testOnboardingFinalReport(t *testing.T, stalled bool, timeout time.Duration, ipv6 bool) {
	t.Helper()
	config := testBootstrapConfig(t)
	config.RequestLimits.ProgressTimeout = timeout
	config.DeviceSerialNumber = "test-device"
	root := filepath.Dir(config.StateDir)
	scriptLog := filepath.Join(root, "script-runs")
	t.Setenv("SZTP_TEST_SCRIPT_LOG", scriptLog)
	now := time.Now()
	cert, key, _ := certificateFixture(t, "device", now.Add(-time.Hour), now.Add(time.Hour))
	config.DeviceEndEntityCert = writeCertificateFixture(t, root, "client.pem", cert)
	config.DevicePrivateKey = writeCertificateFixture(t, root, "client-key.pem", key)
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(cert) {
		t.Fatal("client CA fixture is invalid")
	}
	info, err := json.Marshal(map[string]any{
		"ietf-sztp-conveyed-info:onboarding-information": map[string]string{
			"configuration": base64.StdEncoding.EncodeToString([]byte("test configuration\n")),
			"pre-configuration-script": base64.StdEncoding.EncodeToString([]byte(
				"set -eu\ntest -f ./post-configuration.sh\ncat ./configuration > ./copied-configuration\n" +
					"printf 'pre\\n' >> \"$SZTP_TEST_SCRIPT_LOG\"\nprintf 'pre stdout\\n'\nprintf 'pre stderr\\n' >&2\ncd /\n")),
			"post-configuration-script": base64.StdEncoding.EncodeToString([]byte(
				"set -eu\ntest \"$(cat ./configuration)\" = \"$(cat ./copied-configuration)\"\n" +
					"printf 'post\\n' >> \"$SZTP_TEST_SCRIPT_LOG\"\nprintf 'post stdout\\n'\nprintf 'post stderr\\n' >&2\n")),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	response := conveyedResponseFixture(t, info)
	var requests, completeReports atomic.Int32
	var acceptReport atomic.Bool
	release := make(chan struct{})
	var workspace string
	const restconf = "/custom-restconf"
	const progressPath = restconf + "/operations/ietf-sztp-bootstrap-server:report-progress"
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		serial, password, ok := r.BasicAuth()
		if !ok || serial != "test-device" || password != config.DevicePassword {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case restconf + "/operations/ietf-sztp-bootstrap-server:get-bootstrapping-data":
			_ = json.NewEncoder(w).Encode(response)
		case progressPath:
			var progress sztpclient.ProgressJSON
			if err := json.NewDecoder(r.Body).Decode(&progress); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if progress.IetfSztpBootstrapServerInput.ProgressType == "bootstrap-complete" {
				completeReports.Add(1)
				data, err := os.ReadFile(filepath.Join(config.StateDir, onboardingStateFile))
				var state onboardingState
				if err != nil || json.Unmarshal(data, &state) != nil || state.ReportStatus != reportPending {
					t.Error("final POST preceded durable pending completion")
				}
				if _, err := os.Stat(workspace); !os.IsNotExist(err) {
					t.Errorf("report started before workspace cleanup: %v", err)
				}
				if !acceptReport.Load() {
					if stalled {
						select {
						case <-r.Context().Done():
						case <-release:
						}
						return
					}
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected target request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	if ipv6 {
		listener, err := net.Listen("tcp6", "[::1]:0")
		if err != nil {
			server.Close()
			t.Fatalf("listen on IPv6 loopback: %v", err)
		}
		if err := server.Listener.Close(); err != nil {
			listener.Close()
			t.Fatal(err)
		}
		server.Listener = listener
	}
	server.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: clientCAs}
	server.StartTLS()
	defer func() { close(release); server.Close() }()
	host, portText, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "https://"))
	port, _ := strconv.Atoi(portText)
	redirect := conveyedResponseFixture(t, []byte(fmt.Sprintf(`{"ietf-sztp-conveyed-info:redirect-information":{"bootstrap-server":[{"address":%q,"port":%d}]}}`, host, port)))
	var originRequests atomic.Int32
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originRequests.Add(1)
		switch r.URL.Path {
		case "/.well-known/host-meta":
			_, _ = w.Write([]byte(`<XRD xmlns="http://docs.oasis-open.org/ns/xri/xrd-1.0"><Link rel="restconf" href="/custom-restconf"/></XRD>`))
		case restconf + "/operations/ietf-sztp-bootstrap-server:get-bootstrapping-data":
			_ = json.NewEncoder(w).Encode(redirect)
		case progressPath:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected origin request: %s", r.URL.Path)
		}
	}))
	defer origin.Close()
	config.Server = origin.URL
	config.BootstrapTrustAnchor = writeCertificateFixture(t, root, "server-ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	var logs bytes.Buffer
	logger := tmpTestLogger(&logs)
	bootstrapCalls := 0
	bootstrap := func(ctx context.Context) (sztpclient.CompletionReport, error) {
		bootstrapCalls++
		workspace = config.TmpFolder
		client := sztpclient.NewClient(logger, false, config.Server, workspace, config.DeviceSerialNumber, config.DevicePassword, config.DevicePrivateKey, config.DeviceEndEntityCert, config.BootstrapTrustAnchor)
		return client.RunBootstrapSequence(ctx)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	send := completionReporter(logger, config)
	var reportErr error
	started := time.Now()
	err = runOnboarding(ctx, logger, config, bootstrap, func(ctx context.Context, report sztpclient.CompletionReport) error {
		reportErr = send(ctx, report)
		cancel() // Simulate stopping the process after the first failed attempt.
		return reportErr
	})
	if !errors.Is(err, context.Canceled) || reportErr == nil {
		t.Fatalf("first report error=%v, onboarding=%v\n%s", reportErr, err, &logs)
	}
	if stalled {
		want := timeout
		if want == 0 {
			want = 30 * time.Second
		}
		if elapsed := time.Since(started); elapsed < want || elapsed > want+15*time.Second {
			t.Fatalf("stalled report completed after %s, want the %s deadline", elapsed, want)
		}
		if !errors.Is(reportErr, context.DeadlineExceeded) {
			t.Fatalf("stalled report error=%v", reportErr)
		}
	}
	pending := readTestState(t, config.StateDir)
	if pending.Version != 2 || pending.ReportStatus != reportPending || pending.Report.Endpoint != server.URL+progressPath {
		t.Fatalf("pending state=%+v", pending)
	}
	before, originBefore := requests.Load(), originRequests.Load()
	config.Server = "ftp://unused-invalid-server"
	config.DeviceSerialNumber = "changed-config-serial"
	acceptReport.Store(true)
	if err := runOnboarding(context.Background(), logger, config, bootstrap, send); err != nil {
		t.Fatalf("resume: %v\n%s", err, &logs)
	}
	if requests.Load() != before+1 || originRequests.Load() != originBefore || bootstrapCalls != 1 || completeReports.Load() != 2 {
		t.Fatal("resumed onboarding did not send exactly one report to the saved target")
	}
	ack := readTestState(t, config.StateDir)
	if ack.ReportStatus != reportAcknowledged || ack.CompletedAt != pending.CompletedAt {
		t.Fatalf("acknowledged state=%+v", ack)
	}
	if err := runOnboarding(context.Background(), logger, config, bootstrap, send); err != nil || requests.Load() != before+1 {
		t.Fatalf("acknowledged start contacted server: %v", err)
	}
	data, err := os.ReadFile(scriptLog)
	if err != nil || string(data) != "pre\npost\n" {
		t.Fatalf("scripts did not execute exactly once: %q (%v)", data, err)
	}
	for _, want := range []string{"pre stdout", "pre stderr", "post stdout", "post stderr", "stream=stdout", "stream=stderr"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("retained logs lack %q", want)
		}
	}
}

func readTestState(t *testing.T, dir string) *onboardingState {
	t.Helper()
	store, err := openOnboardingStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	state, err := store.Read()
	if err != nil || state == nil {
		t.Fatalf("state=%v (%v)", state, err)
	}
	return state
}

func TestCompletionReportCancellationInterruptsRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	release := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer func() { close(release); server.Close() }()
	config := testBootstrapConfig(t)
	root := filepath.Dir(config.StateDir)
	now := time.Now()
	cert, key, _ := certificateFixture(t, "device", now.Add(-time.Hour), now.Add(time.Hour))
	config.DeviceEndEntityCert = writeCertificateFixture(t, root, "client.pem", cert)
	config.DevicePrivateKey = writeCertificateFixture(t, root, "client-key.pem", key)
	config.BootstrapTrustAnchor = writeCertificateFixture(t, root, "server-ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	report := sztpclient.CompletionReport{Endpoint: server.URL + "/report", SerialNumber: "serial"}
	start := time.Now()
	err := completionReporter(tmpTestLogger(new(bytes.Buffer)), config)(ctx, report)
	if !errors.Is(err, context.Canceled) || time.Since(start) > 5*time.Second {
		t.Fatalf("request was not interrupted promptly: %v", err)
	}
}

func conveyedResponseFixture(t *testing.T, info []byte) sztpclient.BootstrapServerPostOutput {
	t.Helper()
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
	var response sztpclient.BootstrapServerPostOutput
	response.IetfSztpBootstrapServerOutput.ConveyedInformation = base64.StdEncoding.EncodeToString(conveyed)
	return response
}

func TestOnboardingConveyedInformationValidation(t *testing.T) {
	const empty = `{"ietf-sztp-conveyed-info:onboarding-information":{}}`
	pre := base64.StdEncoding.EncodeToString([]byte("printf 'pre\\n' >> \"$SZTP_TEST_SCRIPT_LOG\"\n"))
	post := base64.StdEncoding.EncodeToString([]byte("cat ./configuration >/dev/null\nprintf 'post\\n' >> \"$SZTP_TEST_SCRIPT_LOG\"\n"))
	scriptContents := fmt.Sprintf(`{"pre-configuration-script":%q}`, pre)
	duplicateOnboarding := fmt.Sprintf(`{"ietf-sztp-conveyed-info:onboarding-information":%s,"ietf-sztp-conveyed-info:onboarding-information":{}}`, scriptContents)
	deferredConfiguration := func(handling string) string {
		return fmt.Sprintf(`{"ietf-sztp-conveyed-info:onboarding-information":{"configuration-handling":%q,"configuration":%q,"pre-configuration-script":%q,"post-configuration-script":%q,"boot-image":{"download-uri":["BOOT_IMAGE_URL"]}}}`,
			handling, base64.StdEncoding.EncodeToString([]byte("script input\n")), pre, post)
	}
	for _, tc := range []struct {
		name, payload string
		redirect      bool
		wantSuccess   bool
		wantError     string
		wantScripts   bool
	}{
		{name: "missing envelope", payload: `{}`},
		{name: "null document", payload: `null`},
		{name: "array document", payload: `[]`},
		{name: "null onboarding", payload: `{"ietf-sztp-conveyed-info:onboarding-information":null}`},
		{name: "unknown envelope", payload: `{"unrelated":{}}`},
		{name: "mixed envelopes", payload: `{"ietf-sztp-conveyed-info:onboarding-information":{},"ietf-sztp-conveyed-info:redirect-information":{}}`},
		{name: "duplicate onboarding", payload: duplicateOnboarding, wantError: "expected exactly one"},
		{name: "duplicate onboarding ending in null", payload: fmt.Sprintf(`{"ietf-sztp-conveyed-info:onboarding-information":%s,"ietf-sztp-conveyed-info:onboarding-information":null}`, scriptContents), wantError: "expected exactly one"},
		{name: "duplicate onboarding starting with null", payload: fmt.Sprintf(`{"ietf-sztp-conveyed-info:onboarding-information":null,"ietf-sztp-conveyed-info:onboarding-information":%s}`, scriptContents), wantError: "expected exactly one"},
		{name: "escaped duplicate onboarding", payload: fmt.Sprintf(`{"ietf-sztp-conveyed-info:onboarding-information":%s,"\u0069etf-sztp-conveyed-info:onboarding-information":{}}`, scriptContents), wantError: "expected exactly one"},
		{name: "duplicate redirect", payload: `{"ietf-sztp-conveyed-info:redirect-information":{},"ietf-sztp-conveyed-info:redirect-information":{}}`, wantError: "expected exactly one"},
		{name: "unknown onboarding field", payload: `{"ietf-sztp-conveyed-info:onboarding-information":{"unexpected":true}}`},
		{name: "wrong onboarding type", payload: `{"ietf-sztp-conveyed-info:onboarding-information":[]}`},
		{name: "trailing document", payload: empty + ` {}`},
		{name: "trailing null", payload: empty + ` null`},
		{name: "trailing garbage", payload: empty + ` !`},
		{name: "missing closing brace", payload: strings.TrimSuffix(empty, "}")},
		{name: "null redirect", payload: `{"ietf-sztp-conveyed-info:redirect-information":null}`},
		{name: "empty redirect", payload: `{"ietf-sztp-conveyed-info:redirect-information":{}}`},
		{name: "empty redirect list", payload: `{"ietf-sztp-conveyed-info:redirect-information":{"bootstrap-server":[]}}`},
		{name: "invalid redirect entry", payload: `{"ietf-sztp-conveyed-info:redirect-information":{"bootstrap-server":[{}]}}`},
		{name: "redirect only", redirect: true},
		{name: "redirect to missing envelope", redirect: true, payload: `{}`},
		{name: "redirect to null onboarding", redirect: true, payload: `{"ietf-sztp-conveyed-info:onboarding-information":null}`},
		{name: "redirect to duplicate onboarding", redirect: true, payload: duplicateOnboarding, wantError: "expected exactly one"},
		{name: "explicit empty onboarding", payload: empty, wantSuccess: true},
		{name: "surrounding whitespace", payload: " \n" + empty + "\t\n", wantSuccess: true},
		{name: "redirect to explicit empty onboarding", redirect: true, payload: empty, wantSuccess: true},
		{name: "deferred boot image", payload: `{"ietf-sztp-conveyed-info:onboarding-information":{"boot-image":{"download-uri":["BOOT_IMAGE_URL"]}}}`, wantSuccess: true},
		{name: "deferred merge with scripts", payload: deferredConfiguration("merge"), wantSuccess: true, wantScripts: true},
		{name: "deferred replace with scripts", payload: deferredConfiguration("replace"), wantSuccess: true, wantScripts: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := testBootstrapConfig(t)
			config.DeviceSerialNumber = "test-device"
			root := filepath.Dir(config.StateDir)
			now := time.Now()
			cert, key, _ := certificateFixture(t, "device", now.Add(-time.Hour), now.Add(time.Hour))
			config.DeviceEndEntityCert = writeCertificateFixture(t, root, "client.pem", cert)
			config.DevicePrivateKey = writeCertificateFixture(t, root, "key.pem", key)
			scriptLog := filepath.Join(root, "script-runs")
			t.Setenv("SZTP_TEST_SCRIPT_LOG", scriptLog)
			var dataRequests, completeReports atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/.well-known/host-meta":
					_, _ = w.Write([]byte(`<XRD xmlns="http://docs.oasis-open.org/ns/xri/xrd-1.0"><Link rel="restconf" href="/restconf"/></XRD>`))
				case "/restconf/operations/ietf-sztp-bootstrap-server:get-bootstrapping-data":
					n := dataRequests.Add(1)
					payload := strings.ReplaceAll(tc.payload, "BOOT_IMAGE_URL", "https://"+r.Host+"/boot-image")
					if tc.redirect && (n == 1 || payload == "") {
						host, portText, err := net.SplitHostPort(r.Host)
						if err != nil {
							t.Error(err)
							w.WriteHeader(http.StatusInternalServerError)
							return
						}
						port, err := strconv.Atoi(portText)
						if err != nil {
							t.Error(err)
							return
						}
						payload = fmt.Sprintf(`{"ietf-sztp-conveyed-info:redirect-information":{"bootstrap-server":[{"address":%q,"port":%d}]}}`, host, port)
					}
					_ = json.NewEncoder(w).Encode(conveyedResponseFixture(t, []byte(payload)))
				case "/restconf/operations/ietf-sztp-bootstrap-server:report-progress":
					var progress sztpclient.ProgressJSON
					if err := json.NewDecoder(r.Body).Decode(&progress); err != nil {
						t.Error(err)
					}
					if progress.IetfSztpBootstrapServerInput.ProgressType == "bootstrap-complete" {
						completeReports.Add(1)
					}
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			config.Server = server.URL
			config.BootstrapTrustAnchor = writeCertificateFixture(t, root, "server-ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
			var logs bytes.Buffer
			logger := tmpTestLogger(&logs)
			err := runOnboarding(context.Background(), logger, config, func(ctx context.Context) (sztpclient.CompletionReport, error) {
				client := sztpclient.NewClient(logger, false, config.Server, config.TmpFolder, config.DeviceSerialNumber, config.DevicePassword, config.DevicePrivateKey, config.DeviceEndEntityCert, config.BootstrapTrustAnchor)
				return client.RunBootstrapSequence(ctx)
			}, completionReporter(logger, config))
			if (err == nil) != tc.wantSuccess {
				t.Fatalf("bootstrap error = %v, want success=%t\n%s", err, tc.wantSuccess, &logs)
			}
			if tc.wantError != "" && (err == nil || !strings.Contains(err.Error(), tc.wantError)) {
				t.Fatalf("bootstrap error = %v, want %q", err, tc.wantError)
			}
			scriptRuns, scriptErr := os.ReadFile(scriptLog)
			if tc.wantScripts {
				if scriptErr != nil || string(scriptRuns) != "pre\npost\n" {
					t.Fatalf("script runs = %q (%v), want pre then post", scriptRuns, scriptErr)
				}
			} else if !os.IsNotExist(scriptErr) {
				t.Fatalf("unexpected script execution: %q (%v)", scriptRuns, scriptErr)
			}
			_, stateErr := os.Stat(filepath.Join(config.StateDir, onboardingStateFile))
			if tc.wantSuccess {
				if stateErr != nil || completeReports.Load() != 1 {
					t.Fatalf("successful onboarding was not recorded: state=%v reports=%d", stateErr, completeReports.Load())
				}
			} else {
				if !os.IsNotExist(stateErr) || completeReports.Load() != 0 {
					t.Fatalf("invalid payload marked completion: state=%v reports=%d", stateErr, completeReports.Load())
				}
				entries, err := os.ReadDir(config.TmpFolder)
				if err != nil || len(entries) != 0 {
					t.Fatalf("invalid payload prepared script artifacts: %v (%v)", entries, err)
				}
			}
			wantRequests := int32(1)
			if tc.redirect {
				wantRequests = 2
			}
			if dataRequests.Load() != wantRequests {
				t.Fatalf("bootstrap data requests=%d, want %d", dataRequests.Load(), wantRequests)
			}
		})
	}
}
