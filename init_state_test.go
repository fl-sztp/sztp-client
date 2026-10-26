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
			config, err := getConfig()
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
			config, err := getConfig()
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
