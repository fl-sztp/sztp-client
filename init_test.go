// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fl-sztp-client/sztpclient"
)

func readHTTPLimitsConfig(t *testing.T, contents string, args ...string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	previousFlags, previousArgs := flag.CommandLine, os.Args
	defer func() { flag.CommandLine, os.Args = previousFlags, previousArgs }()
	flag.CommandLine = flag.NewFlagSet("http-limits-test", flag.ContinueOnError)
	os.Args = append([]string{"http-limits-test", "--config", path}, args...)
	return getConfig()
}

func TestHTTPRequestLimitsConfig(t *testing.T) {
	defaults := sztpclient.DefaultHTTPRequestLimits()
	c, err := readHTTPLimitsConfig(t, "{}")
	if err != nil || c.RequestLimits != defaults {
		t.Fatalf("default config = %+v, error=%v", c, err)
	}
	for _, option := range []struct {
		name    string
		timeout func(*sztpclient.HTTPRequestLimits) *time.Duration
		bytes   func(*sztpclient.HTTPRequestLimits) *int64
	}{
		{
			name: "host-meta-timeout",
			timeout: func(limits *sztpclient.HTTPRequestLimits) *time.Duration {
				return &limits.HostMetaTimeout
			},
		},
		{
			name: "bootstrap-timeout",
			timeout: func(limits *sztpclient.HTTPRequestLimits) *time.Duration {
				return &limits.BootstrapTimeout
			},
		},
		{
			name: "progress-timeout",
			timeout: func(limits *sztpclient.HTTPRequestLimits) *time.Duration {
				return &limits.ProgressTimeout
			},
		},
		{
			name: "host-meta-max-response-bytes",
			bytes: func(limits *sztpclient.HTTPRequestLimits) *int64 {
				return &limits.HostMetaMaxResponseBytes
			},
		},
		{
			name: "bootstrap-max-response-bytes",
			bytes: func(limits *sztpclient.HTTPRequestLimits) *int64 {
				return &limits.BootstrapMaxResponseBytes
			},
		},
		{
			name: "progress-max-response-bytes",
			bytes: func(limits *sztpclient.HTTPRequestLimits) *int64 {
				return &limits.ProgressMaxResponseBytes
			},
		},
	} {
		t.Run(option.name, func(t *testing.T) {
			yamlValue, cliValue := "4096", "8192"
			if option.timeout != nil {
				yamlValue, cliValue = "45s", "2m"
			}
			for _, source := range []string{"YAML", "CLI", "override invalid YAML"} {
				t.Run(source, func(t *testing.T) {
					contents := option.name + ": " + yamlValue + "\n"
					var args []string
					if source != "YAML" {
						args = []string{"--" + option.name, cliValue}
					}
					if source == "override invalid YAML" {
						contents = option.name + ": invalid\n"
					}
					got, err := readHTTPLimitsConfig(t, contents, args...)
					if err != nil {
						t.Fatal(err)
					}
					want := sztpclient.DefaultHTTPRequestLimits()
					if option.timeout != nil {
						*option.timeout(&want) = 45 * time.Second
						if source != "YAML" {
							*option.timeout(&want) = 2 * time.Minute
						}
					} else {
						*option.bytes(&want) = 4096
						if source != "YAML" {
							*option.bytes(&want) = 8192
						}
					}
					if got.RequestLimits != want {
						t.Fatalf("limits=%+v, want %+v", got.RequestLimits, want)
					}
				})
			}
			invalid := []string{"", "0", "-1", "invalid", "9223372036854775808"}
			if option.timeout != nil {
				invalid = append(invalid, "0s", "-1s", "999999999999999999h")
			} else {
				invalid = append(invalid, "1.5", fmt.Sprint(^uint(0)>>1))
			}
			for _, value := range invalid {
				for _, cli := range []bool{false, true} {
					t.Run(fmt.Sprintf("invalid=%q/CLI=%t", value, cli), func(t *testing.T) {
						contents := fmt.Sprintf("%s: %q\n", option.name, value)
						var args []string
						if cli {
							contents = option.name + ": " + yamlValue + "\n"
							args = []string{"--" + option.name, value}
						}
						if _, err := readHTTPLimitsConfig(t, contents, args...); err == nil || !strings.Contains(err.Error(), option.name) {
							t.Fatalf("error=%v, want contextual rejection", err)
						}
					})
				}
			}
		})
	}
}

// Bootstrap validation now happens after the persistent-state check. Existing
// configuration tests exercise that validation without filesystem state.
func getConfigForBootstrapTest() (*Config, error) {
	config, err := getConfigWithPaths([]string{"./config.yaml"})
	if err == nil && !config.DiscoverOnly && !config.ResetOnboarding {
		err = validateBootstrapConfig(config)
	}
	return config, err
}

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

			config, err := getConfigForBootstrapTest()
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

func TestConfigTmpFolder(t *testing.T) {
	for _, tc := range []struct {
		name, yamlTmp, cliTmp, want string
		useCLI, discoverOnly        bool
	}{
		{name: "omitted tmp selects automatic default"},
		{name: "empty YAML tmp selects automatic default", yamlTmp: "tmp: \"\"\n"},
		{name: "YAML tmp is preserved", yamlTmp: "tmp: requested-yaml\n", want: "requested-yaml"},
		{name: "CLI overrides YAML", yamlTmp: "tmp: requested-yaml\n", useCLI: true, cliTmp: "requested-cli", want: "requested-cli"},
		{name: "empty CLI preserves YAML", yamlTmp: "tmp: requested-yaml\n", useCLI: true, want: "requested-yaml"},
		{name: "discovery uses automatic default without creating it", discoverOnly: true},
		{name: "discovery preserves configured tmp without creating it", discoverOnly: true, useCLI: true, cliTmp: "requested-discovery", want: "requested-discovery"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			t.Setenv("TMPDIR", filepath.Join(root, "automatic-parent"))
			fixture := tc.yamlTmp + "password: test-password\nprivate-key: key.pem\nend-entity-cert: cert.pem\ntrust-anchor-cert: ca.pem\n"
			if err := os.WriteFile("config.yaml", []byte(fixture), 0600); err != nil {
				t.Fatal(err)
			}
			previousFlags, previousArgs := flag.CommandLine, os.Args
			flag.CommandLine = flag.NewFlagSet("tmp-config-test", flag.ContinueOnError)
			os.Args = []string{"tmp-config-test", "--config", "config.yaml"}
			if tc.useCLI {
				os.Args = append(os.Args, "--tmp", tc.cliTmp)
			}
			if tc.discoverOnly {
				os.Args = append(os.Args, "--discover-only")
			}
			t.Cleanup(func() { flag.CommandLine, os.Args = previousFlags, previousArgs })
			config, err := getConfigForBootstrapTest()
			if err != nil {
				t.Fatal(err)
			}
			if config.TmpFolder != tc.want || config.DiscoverOnly != tc.discoverOnly {
				t.Fatalf("tmp=%q discover-only=%v, want %q/%v", config.TmpFolder, config.DiscoverOnly, tc.want, tc.discoverOnly)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 1 || entries[0].Name() != "config.yaml" {
				t.Fatalf("configuration parsing created files: %v (%v)", entries, err)
			}
		})
	}
}
