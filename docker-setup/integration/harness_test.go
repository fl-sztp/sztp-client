//go:build linux && dhcp_integration

// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"debug/elf"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"fl-sztp-client/docker-setup/integration/internal/control"
)

const productionBinary = "/usr/local/bin/fl-sztp-client"

type integrationDriver struct {
	api       *fixtureAPI
	clientMAC net.HardwareAddr
}

type fixtureAPI struct {
	base   string
	client *http.Client
}

func newDriver(t *testing.T, ctx context.Context) *integrationDriver {
	t.Helper()
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Fatalf("integration runtime is %s/%s, require linux/amd64", runtime.GOOS, runtime.GOARCH)
	}
	binary, err := elf.Open(productionBinary)
	if err != nil {
		t.Fatalf("open production ELF: %v", err)
	}
	machine, class := binary.Machine, binary.Class
	if err := binary.Close(); err != nil {
		t.Fatalf("close production ELF: %v", err)
	}
	if machine != elf.EM_X86_64 || class != elf.ELFCLASS64 {
		t.Fatalf("production ELF is %s/%s, require 64-bit x86-64", machine, class)
	}
	iface, err := net.InterfaceByName("eth0")
	if err != nil || iface == nil || len(iface.HardwareAddr) != 6 {
		t.Fatalf("read tester eth0 Ethernet identity: %v", err)
	}
	base := os.Getenv("DHCP_FIXTURE_URL")
	if base == "" {
		base = "http://fixture:8081"
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.Host != "fixture:8081" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		t.Fatal("DHCP_FIXTURE_URL must be the internal http://fixture:8081 control service")
	}
	transport := &http.Transport{Proxy: nil}
	t.Cleanup(transport.CloseIdleConnections)
	api := &fixtureAPI{
		base: strings.TrimSuffix(base, "/"),
		client: &http.Client{
			Transport: transport, Timeout: 2 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("control redirects are forbidden") },
		},
	}
	if _, err := api.request(ctx, http.MethodGet, "/health", nil, http.StatusOK); err != nil {
		t.Fatalf("fixture readiness: %v", err)
	}
	if err := os.MkdirAll("/artifacts", 0755); err != nil {
		t.Fatalf("create artifact root: %v", err)
	}
	return &integrationDriver{api: api, clientMAC: append(net.HardwareAddr(nil), iface.HardwareAddr...)}
}

func (api *fixtureAPI) request(ctx context.Context, method, path string, body []byte, status int) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, api.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := api.client.Do(req)
	if err != nil {
		return nil, err
	}
	const maxControlBody = 8 << 20
	data, readErr := io.ReadAll(io.LimitReader(response.Body, maxControlBody+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		return data, fmt.Errorf("read/close control response: %v", errors.Join(readErr, closeErr))
	}
	if len(data) > maxControlBody {
		return data, errors.New("control response exceeds 8 MiB")
	}
	if response.StatusCode != status {
		return data, fmt.Errorf("%s %s: HTTP %d, want %d: %s", method, path, response.StatusCode, status, data)
	}
	return data, nil
}

func (api *fixtureAPI) events(ctx context.Context) ([]control.Event, []byte, error) {
	data, err := api.request(ctx, http.MethodGet, "/events", nil, http.StatusOK)
	if err != nil {
		return nil, data, err
	}
	var events []control.Event
	if err := json.Unmarshal(data, &events); err != nil {
		return nil, data, fmt.Errorf("decode fixture events: %w", err)
	}
	if events == nil {
		return nil, data, errors.New("fixture events must be a JSON array")
	}
	return events, data, nil
}

func (d *integrationDriver) run(t *testing.T, suite context.Context, c integrationCase) {
	t.Helper()
	ctx, cancel := context.WithTimeout(suite, c.timeout)
	defer cancel()
	artifacts := filepath.Join("/artifacts", strings.ReplaceAll(t.Name(), "/", "__"))
	if err := os.MkdirAll(artifacts, 0755); err != nil {
		t.Fatalf("create case artifacts: %v", err)
	}
	writeArtifact := func(name string, data []byte) {
		if err := os.WriteFile(filepath.Join(artifacts, name), data, 0644); err != nil {
			t.Errorf("write %s artifact: %v", name, err)
		}
	}
	var process *clientProcess
	var events []control.Event
	var finalErr error
	var finishOnce sync.Once
	finish := func() {
		finishOnce.Do(func() {
			// Wait before querying final events or allowing the next fixture reset.
			if process != nil {
				if err := process.stop(); err != nil {
					t.Errorf("stop/wait production client: %v", err)
				}
				writeArtifact("stdout.log", process.stdout.snapshot())
				writeArtifact("stderr.log", process.stderr.snapshot())
			} else {
				writeArtifact("stdout.log", nil)
				writeArtifact("stderr.log", nil)
			}
			captureCtx, captureCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer captureCancel()
			var raw []byte
			events, raw, finalErr = d.api.events(captureCtx)
			writeArtifact("events.json", raw)
			if finalErr != nil {
				writeArtifact("events-error.txt", []byte(finalErr.Error()+"\n"))
				t.Errorf("capture final fixture events: %v", finalErr)
			}
		})
	}
	defer finish()
	if err := c.scenario.Validate(); err != nil {
		t.Fatalf("invalid integration scenario: %v", err)
	}
	scenario, err := json.MarshalIndent(c.scenario, "", "  ")
	if err != nil {
		t.Fatalf("encode scenario: %v", err)
	}
	writeArtifact("scenario.json", scenario)
	if _, err := d.api.request(ctx, http.MethodPut, "/scenario", scenario, http.StatusNoContent); err != nil {
		t.Fatalf("configure fixture: %v", err)
	}
	initial, _, err := d.api.events(ctx)
	if err != nil || len(initial) != 0 {
		t.Fatalf("fixture reset must clear events: count=%d error=%v", len(initial), err)
	}
	work := t.TempDir()
	key, cert := writeCredentials(t, work)
	process, err = startClient(work, key, cert)
	if err != nil {
		t.Fatalf("start production client: %v", err)
	}
	if err := process.awaitDiscovery(ctx, c); err != nil {
		t.Errorf("production discovery: %v", err)
	}
	c.timeoutObserved = time.Now()
	if c.silentProtocol != 0 && time.Since(process.started) < 35*time.Second {
		t.Errorf("silent DHCPv%d did not wait the real 35-second production timeout", c.silentProtocol)
	}
	finish()
	if finalErr == nil {
		d.assertTrace(t, c, events)
	}
	if t.Failed() {
		t.Logf("case evidence: %s", artifacts)
	}
}

func writeCredentials(t *testing.T, work string) (string, string) {
	t.Helper()
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate private key: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatalf("encode private key: %v", err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "docker-test"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, pub, private)
	if err != nil {
		t.Fatalf("generate certificate: %v", err)
	}
	key, cert := filepath.Join(work, "key.pem"), filepath.Join(work, "cert.pem")
	for _, file := range []struct {
		name, kind string
		data       []byte
	}{{key, "PRIVATE KEY", keyDER}, {cert, "CERTIFICATE", certDER}} {
		if err := os.WriteFile(file.name, pem.EncodeToMemory(&pem.Block{Type: file.kind, Bytes: file.data}), 0600); err != nil {
			t.Fatalf("write ephemeral credentials: %v", err)
		}
	}
	return key, cert
}

type lockedLog struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedLog) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}

func (b *lockedLog) snapshot() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buffer.Bytes()...)
}

type clientProcess struct {
	cmd            *exec.Cmd
	stdout, stderr *lockedLog
	done           chan struct{}
	started        time.Time
	waitErr        error // Published by closing done; read only after done closes.
	stopOnce       sync.Once
	stopErr        error
}

func startClient(work, key, cert string) (*clientProcess, error) {
	p := &clientProcess{stdout: &lockedLog{}, stderr: &lockedLog{}, done: make(chan struct{})}
	p.cmd = exec.Command(productionBinary,
		"--config", "", "--debug", "--insecure", "--tmp", work,
		"--serial", "docker-test", "--password", "test-only",
		"--private-key", key, "--end-entity-cert", cert, "--trust-anchor-cert", cert)
	p.cmd.Stdout, p.cmd.Stderr = p.stdout, p.stderr
	p.started = time.Now()
	if err := p.cmd.Start(); err != nil {
		return nil, err
	}
	go func() { p.waitErr = p.cmd.Wait(); close(p.done) }()
	return p, nil
}

func (p *clientProcess) stop() error {
	p.stopOnce.Do(func() {
		select {
		case <-p.done:
			return
		default:
		}
		err := p.cmd.Process.Kill()
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			p.stopErr = err
		}
		<-p.done // The only Wait occurs in the goroutine, including after Kill.
	})
	return p.stopErr
}

func (p *clientProcess) awaitDiscovery(ctx context.Context, c integrationCase) error {
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("discovery deadline: %w", err)
		}
		// Check exit before inspecting success logs: a process that died while
		// emitting the marker is not a successful discovery observation.
		select {
		case <-p.done:
			return fmt.Errorf("client exited before discovery was observed: %v", p.waitErr)
		default:
		}
		gotRetry := false
		for _, record := range logRecords(p.stderr.snapshot()) {
			if record["event"] == "bootstrap_server_selected" {
				if c.want == "" || record["server"] != c.want {
					return fmt.Errorf("discovery server %q, want %q", record["server"], c.want)
				}
				if c.minuteRetry && !gotRetry {
					return errors.New("second-round discovery lacked the real retry marker")
				}
				select {
				case <-p.done:
					return fmt.Errorf("client exited during discovery: %v", p.waitErr)
				default:
					return nil
				}
			}
			if record["event"] == "dhcp_retry_scheduled" {
				if record["delay"] != time.Minute.String() {
					return fmt.Errorf("discovery retry delay %q, want %s", record["delay"], time.Minute)
				}
				if c.want == "" {
					select {
					case <-p.done:
						return fmt.Errorf("client exited at first retry: %v", p.waitErr)
					default:
						return nil
					}
				}
				if !c.minuteRetry || gotRetry {
					return fmt.Errorf("unexpected discovery retry: %v", record)
				}
				gotRetry = true
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("discovery deadline: %w", ctx.Err())
		case <-p.done:
			return fmt.Errorf("client exited before discovery: %v", p.waitErr)
		case <-tick.C:
		}
	}
}
