// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stateConfigTestArgs(t *testing.T, args ...string) {
	t.Helper()
	previousFlags, previousArgs := flag.CommandLine, os.Args
	flag.CommandLine = flag.NewFlagSet("state-config-test", flag.ContinueOnError)
	os.Args = append([]string{"state-config-test"}, args...)
	t.Cleanup(func() { flag.CommandLine, os.Args = previousFlags, previousArgs })
}

func TestConfigStateDirectory(t *testing.T) {
	for _, tc := range []struct {
		name, yaml, want, wantErr string
		args                      []string
	}{
		{name: "default", want: defaultStateDir},
		{name: "empty YAML uses default", yaml: "state-dir: ''\n", want: defaultStateDir},
		{name: "YAML", yaml: "state-dir: /custom/state\n", want: "/custom/state"},
		{name: "CLI overrides YAML", yaml: "state-dir: /custom/state\n", args: []string{"--state-dir", "/cli/state"}, want: "/cli/state"},
		{name: "CLI overrides invalid YAML path", yaml: "state-dir: relative\n", args: []string{"--state-dir", "/cli/state"}, want: "/cli/state"},
		{name: "clean absolute path", args: []string{"--state-dir", "/custom//state/"}, want: "/custom/state"},
		{name: "relative CLI", args: []string{"--state-dir", "relative"}, wantErr: "absolute path"},
		{name: "relative YAML", yaml: "state-dir: relative\n", wantErr: "absolute path"},
		{name: "explicit empty CLI", yaml: "state-dir: /custom/state\n", args: []string{"--state-dir", ""}, wantErr: "must not be empty"},
		{name: "diagnostics ignore state path", args: []string{"--discover-only", "--state-dir", "relative"}, want: "relative"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			t.Setenv("STATE_DIRECTORY", "/environment/is/not/implicit")
			if err := os.WriteFile("config.yaml", []byte("debug: false\n"+tc.yaml), 0600); err != nil {
				t.Fatal(err)
			}
			stateConfigTestArgs(t, tc.args...)
			config, err := getConfigWithPaths([]string{"./config.yaml"})
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error=%v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if config.StateDir != tc.want {
				t.Fatalf("state-dir=%q, want %q", config.StateDir, tc.want)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 1 {
				t.Fatalf("config parsing created files: %v (%v)", entries, err)
			}
		})
	}
}

func TestConfigResetMode(t *testing.T) {
	for _, tc := range []struct {
		name, yaml, wantErr string
		args                []string
		wantReset           bool
	}{
		{name: "missing implicit config allowed", args: []string{"--reset-onboarding"}, wantReset: true},
		{name: "disabled config allowed", args: []string{"--reset-onboarding", "--config", ""}, wantReset: true},
		{name: "reset ignores bootstrap requirements", yaml: "server: ftp://invalid\n", args: []string{"--reset-onboarding"}, wantReset: true},
		{name: "explicit missing config is an error", args: []string{"--reset-onboarding", "--config", "missing.yaml"}, wantErr: "error loading config file"},
		{name: "malformed config is an error", yaml: "invalid: [", args: []string{"--reset-onboarding"}, wantErr: "error loading config file"},
		{name: "incompatible modes", args: []string{"--reset-onboarding", "--discover-only"}, wantErr: "cannot be combined"},
		{name: "YAML cannot enable reset", yaml: "reset-onboarding: true\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			if tc.yaml != "" {
				if err := os.WriteFile("config.yaml", []byte(tc.yaml), 0600); err != nil {
					t.Fatal(err)
				}
			}
			stateConfigTestArgs(t, append(tc.args, "--state-dir", filepath.Join(root, "state"))...)
			config, err := getConfigWithPaths([]string{"./config.yaml"})
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error=%v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if config.ResetOnboarding != tc.wantReset {
				t.Fatalf("reset=%v, want %v", config.ResetOnboarding, tc.wantReset)
			}
			if !tc.wantReset && validateBootstrapConfig(config) == nil {
				t.Fatal("YAML reset bypassed bootstrap validation")
			}
			if _, err := os.Stat(config.StateDir); !os.IsNotExist(err) {
				t.Fatalf("config parsing touched state directory: %v", err)
			}
		})
	}
}

func TestConfigSearch(t *testing.T) {
	for _, tc := range []struct {
		name, system, local, want string
		args                      []string
		wantErr                   bool
	}{
		{name: "system wins", system: "state-dir: /system/state\n", local: "state-dir: /local/state\n", want: "/system/state"},
		{name: "local fallback", local: "state-dir: /local/state\n", want: "/local/state"},
		{name: "explicit local overrides system", system: "state-dir: /system/state\n", local: "state-dir: /local/state\n", args: []string{"--config", "config.yaml"}, want: "/local/state"},
		{name: "missing defaults", want: defaultStateDir},
		{name: "disabled", system: "invalid: [", args: []string{"--config", ""}, want: defaultStateDir},
		{name: "explicit missing", system: "state-dir: /system/state\n", args: []string{"--config", "missing.yaml"}, wantErr: true},
		{name: "malformed system does not fall back", system: "invalid: [", local: "state-dir: /local/state\n", wantErr: true},
		{name: "invalid system does not fall back", system: "state-dir: relative\n", local: "state-dir: /local/state\n", wantErr: true},
		{name: "CLI state wins", system: "state-dir: /system/state\n", args: []string{"--state-dir", "/cli/state"}, want: "/cli/state"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			paths := []string{filepath.Join(root, "system.yaml"), filepath.Join(root, "config.yaml")}
			for i, contents := range []string{tc.system, tc.local} {
				if contents != "" {
					if err := os.WriteFile(paths[i], []byte(contents), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			stateConfigTestArgs(t, append([]string{"--reset-onboarding"}, tc.args...)...)
			config, err := getConfigWithPaths(paths)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected configuration error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if config.StateDir != tc.want {
				t.Fatalf("state=%q, want %q", config.StateDir, tc.want)
			}
		})
	}
}

func TestConfigSearchUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	root := t.TempDir()
	path := filepath.Join(root, "system.yaml")
	if err := os.WriteFile(path, []byte("state-dir: /system/state\n"), 0000); err != nil {
		t.Fatal(err)
	}
	stateConfigTestArgs(t, "--reset-onboarding")
	if _, err := getConfigWithPaths([]string{path, filepath.Join(root, "missing.yaml")}); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("error=%v, want unreadable configuration path", err)
	}
}
