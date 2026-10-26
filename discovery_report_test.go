// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"fl-sztp-client/internal/logging"
	"fl-sztp-client/sztpclient"
)

func TestDiscoveryReport(t *testing.T) {
	failure := errors.New("probe failed")
	for _, tc := range []struct {
		name, server6, server4, configured, serial string
		err6, err4, deviceErr                      error
		wantCode                                   int
		want                                       []string
		protocol                                   discoveryProtocol
		stop                                       bool
		wantCalls                                  []string
	}{
		{name: "both accepted prefer v4", server6: "https://v6.example", server4: "https://v4.example", serial: "override", want: []string{`DHCPv4: accepted "https://v4.example"`, `Effective bootstrap server: "https://v4.example" (source: "DHCPv4")`}},
		{name: "v4 accepted", server4: "https://v4.example", serial: "override", want: []string{`Effective bootstrap server: "https://v4.example" (source: "DHCPv4")`}},
		{name: "v6 error preserves v4 success", server6: "https://ignored.example", err6: failure, server4: "https://v4.example", serial: "override", want: []string{`DHCPv6: discovery error: "probe failed"`, `Effective bootstrap server: "https://v4.example"`}},
		{name: "v4 error still probes v6", server6: "https://v6.example", err4: failure, serial: "override", want: []string{`DHCPv4: discovery error: "probe failed"`}},
		{name: "no DHCP does not pass with configured server", configured: "https://configured.example", serial: "override", wantCode: 1, want: []string{`Effective bootstrap server: "https://configured.example" (source: "config: test.yaml")`}},
		{name: "configured server overrides both results", configured: "https://configured.example", server6: "https://v6.example", server4: "https://v4.example", serial: "override", want: []string{`Configured server: "https://configured.example"`, `Effective bootstrap server: "https://configured.example"`}},
		{name: "missing serial still probes both", server6: "https://v6.example", deviceErr: failure, wantCode: 1, want: []string{`Hardware model: "Model"`, `Device discovery error: "probe failed"`}},
		{name: "empty serial fails even without discovery error", server4: "https://v4.example", wantCode: 1},
		{name: "fallback explanation without retry", serial: "override", wantCode: 1, want: []string{`up to 10 rounds`, `fallback "https://xverse.fusionlayer.com:9090"`}},
		{name: "addresses returned with errors are rejected", server6: "https://ignored6.example", server4: "https://ignored4.example", err6: failure, err4: failure, serial: "override", wantCode: 1},
		{name: "DHCP4 selected", protocol: discoverDHCP4, server4: "https://v4.example", server6: "https://ignored6.example", serial: "override", wantCalls: []string{"device", "v4"}, want: []string{`DHCPv6: skipped (not selected)`, `Effective bootstrap server: "https://v4.example" (source: "DHCPv4")`}},
		{name: "DHCP6 selected", protocol: discoverDHCP6, server4: "https://ignored4.example", server6: "https://v6.example", serial: "override", wantCalls: []string{"device", "v6"}, want: []string{`DHCPv4: skipped (not selected)`, `normal bootstrap still tries DHCPv4 before DHCPv6; DHCPv4 was not tested`, `Effective bootstrap server: "https://v6.example" (source: "DHCPv6")`}},
		{name: "unselected DHCP6 success cannot pass", protocol: discoverDHCP4, server6: "https://ignored6.example", serial: "override", wantCalls: []string{"device", "v4"}, wantCode: 1},
		{name: "unselected DHCP4 success cannot pass", protocol: discoverDHCP6, server4: "https://ignored4.example", serial: "override", wantCalls: []string{"device", "v6"}, wantCode: 1},
		{name: "stop skips DHCP6 after DHCP4 success", stop: true, server4: "https://v4.example", server6: "https://ignored6.example", serial: "override", wantCalls: []string{"device", "v4"}, want: []string{`DHCPv6: skipped (bootstrap server already found via DHCPv4)`, `Discovery test: PASS`}},
		{name: "stop continues after empty DHCP4", stop: true, server6: "https://v6.example", serial: "override", want: []string{`DHCPv6: accepted "https://v6.example"`}},
		{name: "stop rejects address with error and continues", stop: true, server4: "https://ignored4.example", err4: failure, server6: "https://v6.example", serial: "override", want: []string{`DHCPv4: discovery error: "probe failed"`, `DHCPv6: accepted "https://v6.example"`}},
		{name: "stop with neither DHCP result fails", stop: true, serial: "override", wantCode: 1},
		{name: "configured server does not trigger stop", stop: true, configured: "https://configured.example", server6: "https://v6.example", serial: "override", want: []string{`DHCPv6: accepted "https://v6.example"`, `Effective bootstrap server: "https://configured.example"`}},
		{name: "configured server without DHCP still fails with stop", stop: true, configured: "https://configured.example", serial: "override", wantCode: 1},
		{name: "stop still fails without serial", stop: true, server4: "https://v4.example", deviceErr: failure, wantCode: 1, wantCalls: []string{"device", "v4"}, want: []string{`DHCPv6: skipped (bootstrap server already found via DHCPv4)`, `Discovery test: FAIL`}},
		{name: "DHCP4 only with stop", protocol: discoverDHCP4, stop: true, server4: "https://v4.example", serial: "override", wantCalls: []string{"device", "v4"}, want: []string{`DHCPv6: skipped (not selected)`}},
		{name: "DHCP6 only with stop", protocol: discoverDHCP6, stop: true, server6: "https://v6.example", serial: "override", wantCalls: []string{"device", "v6"}, want: []string{`DHCPv4: skipped (not selected)`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output, logs bytes.Buffer
			var calls []string
			config := &Config{DiscoverOnly: true, DeviceSerialNumber: tc.serial, SerialSource: "CLI --serial", Server: tc.configured, ServerSource: "config: test.yaml", DevicePassword: "DO-NOT-PRINT-PASSWORD"}
			config.DiscoveryProtocol, config.DiscoverStopOnSuccess = tc.protocol, tc.stop
			info := sztpclient.DeviceInformation{HardwareSerialNumber: "physical", HardwareSerialSource: "product", SerialNumber: tc.serial, SerialSource: "override", HardwareModel: "Model", OSName: "Test OS", OSVersion: "1"}
			code := runDiscoveryReport(&output, logging.New(&logs, false), config,
				func(_ *slog.Logger, override string) (sztpclient.DeviceInformation, error) {
					calls = append(calls, "device")
					if override != tc.serial {
						t.Fatalf("override = %q", override)
					}
					return info, tc.deviceErr
				}, func(*slog.Logger) (string, error) {
					calls = append(calls, "v4")
					return tc.server4, tc.err4
				}, func(*slog.Logger) (string, error) {
					calls = append(calls, "v6")
					return tc.server6, tc.err6
				})
			wantCalls := tc.wantCalls
			if wantCalls == nil {
				wantCalls = []string{"device", "v4", "v6"}
			}
			if code != tc.wantCode || !reflect.DeepEqual(calls, wantCalls) {
				t.Fatalf("exit=%d calls=%v, want %d/%v", code, calls, tc.wantCode, wantCalls)
			}
			if strings.Contains(output.String(), "level=") || !strings.Contains(logs.String(), "level=INFO") || strings.Contains(logs.String(), "Device information\n") {
				t.Fatalf("report/log stream separation lost: report=%s, logs=%s", &output, &logs)
			}
			if !strings.Contains(output.String(), "Bootstrap server selection (based on tested protocols)") {
				t.Fatal("missing selection heading")
			}
			for _, want := range tc.want {
				if !strings.Contains(output.String(), want) {
					t.Errorf("missing %q in report:\n%s", want, output.String())
				}
			}
			if strings.Contains(output.String(), config.DevicePassword) {
				t.Fatal("report contains device password")
			}
			if tc.serial != "" && !strings.Contains(output.String(), `Authentication serial number: "override" (source: "CLI --serial")`) {
				t.Fatal("override/source missing")
			}
			if !strings.Contains(output.String(), `Hardware serial number: "physical" (source: "product")`) {
				t.Fatal("physical serial/source missing")
			}
		})
	}
}

type failingReportWriter struct{}

func (failingReportWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestDiscoveryReportOutputFailure(t *testing.T) {
	calls := 0
	probe := func(*slog.Logger) (string, error) { calls++; return "https://server.example", nil }
	code := runDiscoveryReport(failingReportWriter{}, discoveryTestLogger(), &Config{},
		func(*slog.Logger, string) (sztpclient.DeviceInformation, error) {
			return sztpclient.DeviceInformation{SerialNumber: "serial"}, nil
		}, probe, probe)
	if code != 1 || calls != 2 {
		t.Fatalf("exit=%d probes=%d", code, calls)
	}
}
