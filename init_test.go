/*
SPDX-License-Identifier: Apache-2.0
Copyright (C) 2026 Anssi Söderena

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigFileValues(t *testing.T) {
	for _, tt := range []struct {
		name, insecure, debug   string
		wantInsecure, wantDebug bool
	}{
		{"YAML 1.2 booleans", "true", "false", true, false},
		{"legacy true booleans", "yes", "on", true, true},
		{"legacy false booleans", "off", "no", false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "config.yaml")
			fixture := fmt.Sprintf(`server: "https://bücher.example:8443"
insecure: %s
debug: %s
tmp: "/tmp/sztp test"
serial: "000042"
password: "001234:#"
private-key: "certs/device private.pem"
end-entity-cert: "certs/device.pem"
trust-anchor-cert: "certs/ca.pem"
`, tt.insecure, tt.debug)
			if err := os.WriteFile(configPath, []byte(fixture), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := getConfigFromFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			want := Config{
				Server:               "https://bücher.example:8443",
				AllowInsecure:        tt.wantInsecure,
				UseDebug:             tt.wantDebug,
				TmpFolder:            "/tmp/sztp test",
				DeviceSerialNumber:   "000042",
				DevicePassword:       "001234:#",
				DevicePrivateKey:     "certs/device private.pem",
				DeviceEndEntityCert:  "certs/device.pem",
				BootstrapTrustAnchor: "certs/ca.pem",
			}
			if *got != want {
				t.Fatalf("config file values = %+v, want %+v", *got, want)
			}
		})
	}
}

func TestConfigServerNormalization(t *testing.T) {
	tests := []struct {
		name          string
		server        string
		cli           []string
		want          string
		wantErr       bool
		wantErrDetail string
	}{
		{
			name:   "complete RESTCONF URL in YAML",
			server: "https://bootstrap:8080/restconf/operations/ietf-sztp-bootstrap-server:get-bootstrapping-data",
			want:   "https://bootstrap:8080",
		},
		{
			name:   "HTTP URL from CLI overrides YAML",
			server: "yaml.example.com",
			cli:    []string{"-server", "http://cli.example.com:8080/restconf?source=cli#bootstrap"},
			want:   "https://cli.example.com:8080",
		},
		{
			name:   "valid CLI overrides invalid YAML",
			server: "ftp://invalid.example.com",
			cli:    []string{"-server", "cli.example.com:8443"},
			want:   "https://cli.example.com:8443",
		},
		{name: "empty server enables DHCP"},
		{name: "whitespace server enables DHCP", server: " \t\n "},
		{
			name:   "Unicode hostname in YAML",
			server: "https://bücher_server.example:8443/restconf",
			want:   "https://xn--bcher_server-dlb.example:8443",
		},
		{
			name:   "Unicode hostname from CLI overrides YAML",
			server: "yaml.example.com",
			cli:    []string{"-server", "ab--cd.bücher.example:8443"},
			want:   "https://ab--cd.xn--bcher-kva.example:8443",
		},
		{
			name:   "whitespace CLI enables DHCP",
			server: "yaml.example.com",
			cli:    []string{"-server", " \t "},
		},
		{
			name:   "bare hostname and port",
			server: "bootstrap.example.com:8443",
			want:   "https://bootstrap.example.com:8443",
		},
		{
			name:   "trim whitespace and remove URL components",
			server: " \thttps://bootstrap.example.com:8443/restconf/data?source=config#bootstrap\n ",
			want:   "https://bootstrap.example.com:8443",
		},
		{name: "invalid host", server: "https://bad host.example.com", wantErr: true},
		{name: "missing host", server: "https:///restconf", wantErr: true},
		{name: "invalid scheme", server: "ftp://bootstrap.example.com", wantErr: true},
		{name: "invalid port", server: "bootstrap.example.com:65536", wantErr: true},
		{name: "invalid CLI port", cli: []string{"-server", "https://bootstrap.example.com:abc"}, wantErr: true},
		{name: "invalid YAML hostname", server: "https://bootstrap..example:443", wantErr: true, wantErrDetail: "invalid bootstrap server hostname"},
		{name: "invalid CLI hostname", server: "yaml.example.com", cli: []string{"-server", "https://bootstrap..example:443"}, wantErr: true, wantErrDetail: "invalid bootstrap server hostname"},
		{
			name:   "valid CLI overrides invalid YAML hostname",
			server: "https://bootstrap..example:443",
			cli:    []string{"-server", "cli.example.com:8443"},
			want:   "https://cli.example.com:8443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "config.yaml")
			fixture := fmt.Sprintf("server: %q\npassword: test-password\nprivate-key: test-private-key.pem\nend-entity-cert: test-end-entity-cert.pem\ntrust-anchor-cert: test-trust-anchor-cert.pem\n", tt.server)
			if err := os.WriteFile(configPath, []byte(fixture), 0600); err != nil {
				t.Fatal(err)
			}

			previousFlags, previousArgs := flag.CommandLine, os.Args
			flag.CommandLine = flag.NewFlagSet("config-test", flag.ContinueOnError)
			os.Args = append([]string{"config-test", "-config", configPath}, tt.cli...)
			t.Cleanup(func() {
				flag.CommandLine = previousFlags
				os.Args = previousArgs
			})

			config, err := getConfig()
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "invalid server address") {
					t.Fatalf("getConfig() error = %v, want contextual invalid server address error", err)
				}
				if tt.wantErrDetail != "" && !strings.Contains(err.Error(), tt.wantErrDetail) {
					t.Fatalf("getConfig() error = %v, want detail %q", err, tt.wantErrDetail)
				}
				return
			}
			if err != nil {
				t.Fatalf("getConfig() error = %v", err)
			}
			if config.Server != tt.want {
				t.Errorf("getConfig().Server = %q, want %q", config.Server, tt.want)
			}
		})
	}
}
