//go:build linux && dhcp_integration

// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func startDiscoveryClient(t *testing.T, work string, c *integrationCase) (*clientProcess, error) {
	t.Helper()
	r := c.discoverOnly
	firmware := filepath.Join(work, "firmware")
	if err := os.MkdirAll(firmware, 0755); err != nil {
		t.Fatal(err)
	}
	if !r.missingSerial {
		dmi := filepath.Join(firmware, "sys/class/dmi/id")
		if err := os.MkdirAll(dmi, 0755); err != nil {
			t.Fatal(err)
		}
		for name, value := range map[string]string{"product_serial": "hardware-test\n", "product_name": "Test Model\n"} {
			if err := os.WriteFile(filepath.Join(dmi, name), []byte(value), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	mode := r.mode
	if mode == "" {
		mode = "--discover-only"
	}
	args := []string{mode, "--debug", "--tmp", filepath.Join(work, "must-not-be-created"), "--state-dir", filepath.Join(work, "state-must-not-be-created")}
	if r.stopOnSuccess {
		args = append(args, "--discover-stop-on-success")
	}
	if !r.missingSerial && !r.autoSerial {
		args = append(args, "--serial", "docker-test")
	}
	if r.certificates {
		key, cert := writeCredentials(t, work)
		args = append(args, "--private-key", key, "--end-entity-cert", cert, "--trust-anchor-cert", cert, "--password", "DO-NOT-PRINT-PASSWORD")
	}
	if r.configured {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		c.want = "https://" + listener.Addr().String()
		t.Cleanup(func() {
			defer listener.Close()
			tcp := listener.(*net.TCPListener)
			if err := tcp.SetDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
				t.Error(err)
				return
			}
			conn, err := tcp.Accept()
			if conn != nil {
				conn.Close()
				t.Error("discovery mode contacted the configured bootstrap server")
			}
			var timeout net.Error
			if err != nil && !(errors.As(err, &timeout) && timeout.Timeout()) {
				t.Errorf("bootstrap connection check: %v", err)
			}
		})
		if err := os.WriteFile(filepath.Join(work, "config.yaml"), []byte(fmt.Sprintf("server: %q\n", c.want)), 0600); err != nil {
			t.Fatal(err)
		}
	} else if r.v4 != "" {
		c.want = r.v4
	} else {
		c.want = r.v6
	}
	return startClientProcess(work, args, []string{"GHW_CHROOT=" + firmware, "GHW_DISABLE_TOOLS=1"})
}

func (p *clientProcess) awaitDiscoveryReport(ctx context.Context, c integrationCase, work string) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("discovery report deadline: %w", ctx.Err())
	case <-p.done:
	}
	code := 0
	if p.waitErr != nil {
		var exit *exec.ExitError
		if !errors.As(p.waitErr, &exit) {
			return p.waitErr
		}
		code = exit.ExitCode()
	}
	r := c.discoverOnly
	output := string(p.stdout.snapshot())
	if code != r.exitCode {
		return fmt.Errorf("discovery exit %d, want %d: %s", code, r.exitCode, output)
	}
	wants := []string{"Bootstrap server selection (based on tested protocols)"}
	for _, probe := range []struct{ name, server, skip string }{{"DHCPv4", r.v4, r.skipV4}, {"DHCPv6", r.v6, r.skipV6}} {
		if probe.skip != "" {
			wants = append(wants, fmt.Sprintf("%s: skipped (%s)\n", probe.name, probe.skip))
		} else if probe.server != "" {
			wants = append(wants, fmt.Sprintf("%s: accepted %q\n", probe.name, probe.server))
		} else {
			wants = append(wants, probe.name+": no accepted bootstrap server;")
		}
	}
	if r.skipV4 != "" {
		wants = append(wants, "normal bootstrap still tries DHCPv4 before DHCPv6; DHCPv4 was not tested")
	}
	if c.want != "" {
		source := "DHCPv6"
		if r.configured {
			source = "config: ./config.yaml"
		} else if r.v4 != "" {
			source = "DHCPv4"
		}
		wants = append(wants, fmt.Sprintf("Effective bootstrap server: %q (source: %q)", c.want, source))
	} else {
		wants = append(wants, `fallback "https://xverse.fusionlayer.com:9090"`)
	}
	if r.missingSerial {
		wants = append(wants, "Authentication serial number: not found", "could not determine the device serial number")
	} else {
		wants = append(wants, `Hardware serial number: "hardware-test" (source: "product")`, `Hardware model: "Test Model"`)
		if r.autoSerial {
			wants = append(wants, `Authentication serial number: "hardware-test" (source: "product")`)
		} else {
			wants = append(wants, `Authentication serial number: "docker-test" (source: "CLI --serial")`)
		}
	}
	if r.certificates {
		wants = append(wants, `Subject: "CN=docker-test"`, "key matches certificate", "Trust anchor 1:")
	} else {
		wants = append(wants, "Client certificate/key pair: not configured completely")
	}
	if code == 0 {
		wants = append(wants, "Discovery test: PASS")
	} else {
		wants = append(wants, "Discovery test: FAIL")
	}
	for _, want := range wants {
		if !strings.Contains(output, want) {
			return fmt.Errorf("missing report field %q: %s", want, output)
		}
	}
	logs := string(p.stderr.snapshot())
	for _, forbidden := range []string{"event=bootstrap_server_selected", "Getting root resources", "event=dhcp_retry_scheduled", "Bootstrap failed", "DO-NOT-PRINT-PASSWORD", "-----BEGIN PRIVATE KEY-----"} {
		if strings.Contains(output+logs, forbidden) {
			return fmt.Errorf("unexpected bootstrap or secret output %q", forbidden)
		}
	}
	if _, err := os.Stat(filepath.Join(work, "must-not-be-created")); !os.IsNotExist(err) {
		return fmt.Errorf("discovery mode touched its tmp folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, "state-must-not-be-created")); !os.IsNotExist(err) {
		return fmt.Errorf("discovery mode touched its state folder: %v", err)
	}
	return nil
}
