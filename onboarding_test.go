//go:build linux || darwin

// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fl-sztp-client/sztpclient"
)

func legacyState(serial string) onboardingState {
	return onboardingState{Version: 1, CompletedAt: time.Now().UTC(), SerialNumber: serial}
}

func testCompletionReport(serial string) sztpclient.CompletionReport {
	return sztpclient.CompletionReport{SerialNumber: serial, Endpoint: "https://bootstrap.example/restconf/operations/ietf-sztp-bootstrap-server:report-progress"}
}

func runTestOnboarding(logger *slog.Logger, config *Config, bootstrap func() (string, error)) error {
	return runOnboarding(context.Background(), logger, config, func(context.Context) (sztpclient.CompletionReport, error) {
		serial, err := bootstrap()
		return testCompletionReport(serial), err
	}, func(context.Context, sztpclient.CompletionReport) error { return nil })
}

func testBootstrapConfig(t *testing.T) *Config {
	t.Helper()
	root := t.TempDir()
	return &Config{
		StateDir:       filepath.Join(root, "state"),
		TmpFolder:      filepath.Join(root, "tmp"),
		Server:         "bootstrap.example",
		DevicePassword: "password", DevicePrivateKey: "key.pem",
		DeviceEndEntityCert: "cert.pem", BootstrapTrustAnchor: "ca.pem",
	}
}

func TestOnboardingCompletionSkipsNextStart(t *testing.T) {
	config := testBootstrapConfig(t)
	var logs bytes.Buffer
	logger := tmpTestLogger(&logs)
	calls := 0
	bootstrap := func() (string, error) { calls++; return "hardware-serial", nil }
	if err := runTestOnboarding(logger, config, bootstrap); err != nil {
		t.Fatal(err)
	}
	// No credentials or configuration are needed to recognize local completion.
	next := &Config{StateDir: config.StateDir, TmpFolder: filepath.Join(t.TempDir(), "unused"), Server: "ftp://invalid", configLoadErr: errors.New("missing implicit config")}
	if err := runTestOnboarding(logger, next, bootstrap); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("bootstrap called %d times, want once", calls)
	}
	if _, err := os.Stat(next.TmpFolder); !os.IsNotExist(err) {
		t.Fatalf("skipped start created tmp folder: %v", err)
	}
	if !strings.Contains(logs.String(), "already onboarded") {
		t.Fatalf("missing skip diagnostic: %s", &logs)
	}
	store, err := openOnboardingStore(config.StateDir)
	if err != nil {
		t.Fatalf("successful start retained lock: %v", err)
	}
	defer store.Close()
	state, err := store.Read()
	if err != nil || state == nil || state.SerialNumber != "hardware-serial" {
		t.Fatalf("saved identity=%v (%v)", state, err)
	}
}

func TestOnboardingBootstrapFailureDoesNotMarkCompletion(t *testing.T) {
	config := testBootstrapConfig(t)
	wantErr := errors.New("script failed")
	if err := runTestOnboarding(tmpTestLogger(new(bytes.Buffer)), config, func() (string, error) { return "", wantErr }); !errors.Is(err, wantErr) {
		t.Fatalf("error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(config.StateDir, onboardingStateFile)); !os.IsNotExist(err) {
		t.Fatalf("failed bootstrap saved completion: %v", err)
	}
	if err := runTestOnboarding(tmpTestLogger(new(bytes.Buffer)), config, func() (string, error) { return "serial", nil }); err != nil {
		t.Fatalf("retry after failure: %v", err)
	}
}

func TestOnboardingSaveFailureDoesNotRetryScripts(t *testing.T) {
	config := testBootstrapConfig(t)
	calls := 0
	bootstrap := func() (string, error) {
		calls++
		// Simulate storage becoming unusable after provisioning succeeded.
		return "serial", os.Mkdir(filepath.Join(config.StateDir, onboardingStateFile), 0700)
	}
	err := runTestOnboarding(tmpTestLogger(new(bytes.Buffer)), config, bootstrap)
	if err == nil || !strings.Contains(err.Error(), "publish onboarding state") {
		t.Fatalf("expected completion write failure, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("save failure retried bootstrap %d times", calls)
	}
	if err := runTestOnboarding(tmpTestLogger(new(bytes.Buffer)), config, bootstrap); err == nil {
		t.Fatal("unreadable state allowed bootstrap")
	}
	if calls != 1 {
		t.Fatal("unreadable state retried bootstrap")
	}
}

func TestOnboardingResetThenReprovision(t *testing.T) {
	config := testBootstrapConfig(t)
	logger := tmpTestLogger(new(bytes.Buffer))
	calls := 0
	bootstrap := func() (string, error) { calls++; return "serial", nil }
	if err := runTestOnboarding(logger, config, bootstrap); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(config.StateDir, "keep")
	if err := os.WriteFile(keep, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	reset := &Config{StateDir: config.StateDir, ResetOnboarding: true}
	for range 2 {
		if err := runTestOnboarding(logger, reset, bootstrap); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatal("reset ran bootstrap")
	}
	if data, err := os.ReadFile(keep); err != nil || string(data) != "keep" {
		t.Fatalf("reset removed unrelated file: %q (%v)", data, err)
	}
	if err := runTestOnboarding(logger, config, bootstrap); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("bootstrap calls after reset=%d", calls)
	}
}

func TestOnboardingRejectsCorruptStateBeforeBootstrap(t *testing.T) {
	config := testBootstrapConfig(t)
	store, err := openOnboardingStore(config.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.dir, onboardingStateFile), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	bootstrap := func() (string, error) { t.Fatal("corrupt state ran bootstrap"); return "", nil }
	if err := runTestOnboarding(tmpTestLogger(new(bytes.Buffer)), config, bootstrap); err == nil {
		t.Fatal("corrupt state accepted")
	}
	if _, err := os.Stat(config.TmpFolder); !os.IsNotExist(err) {
		t.Fatalf("corrupt state created tmp: %v", err)
	}
	config.ResetOnboarding = true
	if err := runTestOnboarding(tmpTestLogger(new(bytes.Buffer)), config, bootstrap); err != nil {
		t.Fatalf("reset corrupt state: %v", err)
	}
}

func TestOnboardingRequiresCredentialsOnlyForNewBootstrap(t *testing.T) {
	config := &Config{StateDir: filepath.Join(t.TempDir(), "state"), TmpFolder: filepath.Join(t.TempDir(), "unused")}
	err := runTestOnboarding(tmpTestLogger(new(bytes.Buffer)), config, func() (string, error) { t.Fatal("bootstrap ran without credentials"); return "", nil })
	if err == nil || !strings.Contains(err.Error(), "device password is required") {
		t.Fatalf("missing credentials: %v", err)
	}
	if _, err := os.Stat(config.TmpFolder); !os.IsNotExist(err) {
		t.Fatalf("credential failure created tmp: %v", err)
	}
}

// Exercise the actual entrypoint and its exit status in a separate process.
func TestOnboardingCLIHelper(t *testing.T) {
	encoded := os.Getenv("SZTP_TEST_CLI_ARGS")
	if encoded == "" {
		t.Skip("subprocess helper")
	}
	var args []string
	if err := json.Unmarshal([]byte(encoded), &args); err != nil {
		t.Fatal(err)
	}
	flag.CommandLine = flag.NewFlagSet("sztp-client", flag.ExitOnError)
	os.Args = append([]string{"sztp-client"}, args...)
	main()
}

func onboardingCLI(t *testing.T, workingDir string, args ...string) (string, error) {
	t.Helper()
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestOnboardingCLIHelper$")
	cmd.Env = append(os.Environ(), "SZTP_TEST_CLI_ARGS="+string(encoded))
	cmd.Dir = workingDir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestOnboardingCLILogLevelsAndStreams(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "state")
	store, err := openOnboardingStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(legacyState("serial")); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		extra   []string
		debug   bool
		failure bool
	}{
		{name: "normal"},
		{name: "debug", extra: []string{"--debug"}, debug: true},
		{name: "configuration failure", extra: []string{"--state-dir", "relative"}, failure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"--config", "", "--state-dir", dir}, tc.extra...)
			encoded, err := json.Marshal(args)
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestOnboardingCLIHelper$")
			cmd.Env = append(os.Environ(), "SZTP_TEST_CLI_ARGS="+string(encoded))
			cmd.Dir = root
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err = cmd.Run()
			if tc.failure {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
					t.Fatalf("exit=%v, stderr=%s", err, &stderr)
				}
				if !strings.Contains(stderr.String(), "level=ERROR") || !strings.Contains(stderr.String(), "error=") {
					t.Fatalf("missing startup failure: %s", &stderr)
				}
			} else if err != nil || !strings.Contains(stderr.String(), "level=INFO") {
				t.Fatalf("exit=%v, stderr=%s", err, &stderr)
			}
			if strings.Contains(stderr.String(), "level=DEBUG") != tc.debug {
				t.Fatalf("incorrect debug filtering: %s", &stderr)
			}
			// The Go test subprocess itself may print PASS to stdout.
			if strings.Contains(stdout.String(), "level=") || strings.Contains(stdout.String(), "msg=") {
				t.Fatalf("diagnostics leaked to stdout: %s", &stdout)
			}
			if !strings.Contains(stderr.String(), "time=") || strings.Contains(stderr.String(), "\x1b[") {
				t.Fatalf("unexpected text log format: %s", &stderr)
			}
		})
	}
}

func TestOnboardingCLICompletedResetAndErrors(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "state")
	store, err := openOnboardingStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(legacyState("serial")); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := onboardingCLI(t, root, "--state-dir", dir)
	if err != nil || !strings.Contains(out, "skipping bootstrap") {
		t.Fatalf("completed CLI: %s (%v)", out, err)
	}
	out, err = onboardingCLI(t, root, "--state-dir", dir, "--config", "missing.yaml")
	if err == nil || !strings.Contains(out, "error loading config file") {
		t.Fatalf("explicit config error: %s (%v)", out, err)
	}
	store, err = openOnboardingStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	out, err = onboardingCLI(t, root, "--state-dir", dir, "--reset-onboarding")
	if err == nil || !strings.Contains(out, "already running") {
		t.Fatalf("locked reset: %s (%v)", out, err)
	}
	if state, err := store.Read(); err != nil || state == nil {
		t.Fatalf("locked reset removed state: %v (%v)", state, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	out, err = onboardingCLI(t, root, "--state-dir", dir, "--reset-onboarding")
	if err != nil || !strings.Contains(out, "state reset") {
		t.Fatalf("reset CLI: %s (%v)", out, err)
	}
	out, err = onboardingCLI(t, root, "--state-dir", dir, "--config", "")
	if err == nil || !strings.Contains(out, "device password is required") {
		t.Fatalf("new bootstrap without credentials: %s (%v)", out, err)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("failure exit code: %v", err)
	}
}
