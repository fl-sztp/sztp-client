// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"os"
	"strings"
	"testing"
)

func TestDiscoveryConfig(t *testing.T) {
	for _, tc := range []struct {
		name, yaml, wantErr                                        string
		args                                                       []string
		wantServer, wantSerial, wantServerSource, wantSerialSource string
		wantProtocol                                               discoveryProtocol
		wantStop                                                   bool
	}{
		{name: "no config or credentials", args: []string{"--discover-only"}},
		{name: "DHCP4 without config or credentials", args: []string{"--discover-only-dhcp4"}, wantProtocol: discoverDHCP4},
		{name: "DHCP6 without config or credentials", args: []string{"--discover-only-dhcp6"}, wantProtocol: discoverDHCP6},
		{name: "stop after success with both", args: []string{"--discover-only", "--discover-stop-on-success"}, wantStop: true},
		{name: "stop after success with DHCP4", args: []string{"--discover-only-dhcp4", "--discover-stop-on-success"}, wantProtocol: discoverDHCP4, wantStop: true},
		{name: "stop after success with DHCP6", args: []string{"--discover-only-dhcp6", "--discover-stop-on-success"}, wantProtocol: discoverDHCP6, wantStop: true},
		{name: "false mode does not conflict", args: []string{"--discover-only=false", "--discover-only-dhcp4"}, wantProtocol: discoverDHCP4},
		{name: "both and DHCP4 conflict", args: []string{"--discover-only", "--discover-only-dhcp4"}, wantErr: "mutually exclusive"},
		{name: "both and DHCP6 conflict", args: []string{"--discover-only", "--discover-only-dhcp6"}, wantErr: "mutually exclusive"},
		{name: "DHCP4 and DHCP6 conflict", args: []string{"--discover-only-dhcp4", "--discover-only-dhcp6"}, wantErr: "mutually exclusive"},
		{name: "all modes conflict", args: []string{"--discover-only", "--discover-only-dhcp4", "--discover-only-dhcp6"}, wantErr: "mutually exclusive"},
		{name: "stop requires diagnostic mode", args: []string{"--discover-stop-on-success"}, wantErr: "requires --discover-only"},
		{name: "stop cannot enable diagnostics during reset", args: []string{"--reset-onboarding", "--discover-stop-on-success"}, wantErr: "requires --discover-only"},
		{name: "reset conflicts with both", args: []string{"--reset-onboarding", "--discover-only"}, wantErr: "cannot be combined with --discover-only"},
		{name: "reset conflicts with DHCP4", args: []string{"--reset-onboarding", "--discover-only-dhcp4"}, wantErr: "cannot be combined with --discover-only-dhcp4"},
		{name: "reset conflicts with DHCP6", args: []string{"--reset-onboarding", "--discover-only-dhcp6"}, wantErr: "cannot be combined with --discover-only-dhcp6"},
		{name: "YAML cannot override DHCP choices", yaml: "discover-only-dhcp6: true\ndiscovery-protocol: 2\ndiscover-stop-on-success: true\n", args: []string{"--discover-only-dhcp4"}, wantProtocol: discoverDHCP4},
		{name: "explicit missing file", args: []string{"--discover-only", "--config", "missing.yaml"}, wantErr: "error loading config file"},
		{name: "explicit missing default file", args: []string{"--discover-only", "--config", "./config.yaml"}, wantErr: "error loading config file"},
		{name: "disabled config", yaml: "invalid: [", args: []string{"--discover-only", "--config", ""}},
		{name: "default config loaded", yaml: "server: config.example\nserial: config-serial\n", args: []string{"--discover-only"}, wantServer: "https://config.example", wantSerial: "config-serial", wantServerSource: "config: ./config.yaml", wantSerialSource: "config: ./config.yaml"},
		{name: "CLI overrides config", yaml: "server: config.example\nserial: config-serial\n", args: []string{"--discover-only", "--server", "cli.example:8443/path", "--serial", "cli-serial"}, wantServer: "https://cli.example:8443", wantSerial: "cli-serial", wantServerSource: "CLI --server", wantSerialSource: "CLI --serial"},
		{name: "malformed config", yaml: "invalid: [", args: []string{"--discover-only"}, wantErr: "error loading config file"},
		{name: "invalid configured server", yaml: "server: https://bad..example\n", args: []string{"--discover-only"}, wantErr: "invalid server address"},
		{name: "valid CLI overrides invalid server", yaml: "server: ftp://bad.example\n", args: []string{"--discover-only", "--server", "cli.example"}, wantServer: "https://cli.example", wantServerSource: "CLI --server"},
		{name: "normal mode requires config", wantErr: "error loading config file"},
		{name: "normal mode requires credentials", args: []string{"--config", ""}, wantErr: "device password is required"},
		{name: "YAML cannot enable diagnostic mode", yaml: "discover-only: true\n", wantErr: "device password is required"},
		{name: "YAML cannot enable new diagnostic modes", yaml: "discover-only-dhcp4: true\ndiscover-only-dhcp6: true\ndiscover-stop-on-success: true\n", wantErr: "device password is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if tc.yaml != "" {
				if err := os.WriteFile("config.yaml", []byte(tc.yaml), 0600); err != nil {
					t.Fatal(err)
				}
			}
			previousFlags, previousArgs := flag.CommandLine, os.Args
			flag.CommandLine = flag.NewFlagSet("discovery-config-test", flag.ContinueOnError)
			os.Args = append([]string{"discovery-config-test"}, tc.args...)
			t.Cleanup(func() { flag.CommandLine, os.Args = previousFlags, previousArgs })
			config, err := getConfigForBootstrapTest()
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error=%v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !config.DiscoverOnly || config.DiscoveryProtocol != tc.wantProtocol || config.DiscoverStopOnSuccess != tc.wantStop || config.Server != tc.wantServer || config.DeviceSerialNumber != tc.wantSerial || config.ServerSource != tc.wantServerSource || config.SerialSource != tc.wantSerialSource {
				t.Fatalf("unexpected config: %+v", config)
			}
		})
	}
}
