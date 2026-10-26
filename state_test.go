//go:build linux || darwin

// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fl-sztp-client/sztpclient"
)

func testOnboardingStore(t *testing.T) *onboardingStore {
	t.Helper()
	store, err := openOnboardingStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func pendingTestState() onboardingState {
	report := testCompletionReport("device-42")
	return onboardingState{Version: 2, CompletedAt: time.Now().UTC(), SerialNumber: report.SerialNumber,
		Report: &report, ReportStatus: reportPending}
}

func TestOnboardingPendingAndAcknowledgedState(t *testing.T) {
	store := testOnboardingStore(t)
	want := pendingTestState()
	for _, status := range []string{reportPending, reportAcknowledged} {
		want.ReportStatus = status
		if err := store.Save(want); err != nil {
			t.Fatal(err)
		}
		got, err := store.Read()
		if err != nil || got.ReportStatus != status || got.CompletedAt != want.CompletedAt || *got.Report != *want.Report {
			t.Fatalf("state=%+v (%v), want %+v", got, err, want)
		}
	}
}

func TestOnboardingRejectsIncompleteReportState(t *testing.T) {
	for _, damage := range []string{"no report", "no status", "unknown status", "wrong serial", "relative endpoint", "HTTP endpoint", "credentials in endpoint", "legacy report"} {
		t.Run(damage, func(t *testing.T) {
			store := testOnboardingStore(t)
			state := pendingTestState()
			switch damage {
			case "no report":
				state.Report = nil
			case "no status":
				state.ReportStatus = ""
			case "unknown status":
				state.ReportStatus = "done"
			case "wrong serial":
				state.Report.SerialNumber = "other-device"
			case "relative endpoint":
				state.Report.Endpoint = "/report"
			case "HTTP endpoint":
				state.Report.Endpoint = "http://bootstrap.example/report"
			case "credentials in endpoint":
				state.Report.Endpoint = "https://user:password@bootstrap.example/report"
			case "legacy report":
				state.Version = 1
			}
			data, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(store.dir, onboardingStateFile), data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Read(); err == nil {
				t.Fatal("invalid pending report state accepted")
			}
		})
	}
}

func TestOnboardingPendingReportRequiresCredentialsAndCanReset(t *testing.T) {
	config := testBootstrapConfig(t)
	store, err := openOnboardingStore(config.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(pendingTestState()); err != nil {
		t.Fatal(err)
	}
	store.Close()
	config.DevicePassword = ""
	noBootstrap := func(context.Context) (sztpclient.CompletionReport, error) {
		t.Fatal("pending report reran bootstrap")
		return sztpclient.CompletionReport{}, nil
	}
	noReport := func(context.Context, sztpclient.CompletionReport) error {
		t.Fatal("reported without credentials or during reset")
		return nil
	}
	logger := tmpTestLogger(new(bytes.Buffer))
	err = runOnboarding(context.Background(), logger, config, noBootstrap, noReport)
	if err == nil || !strings.Contains(err.Error(), "device password is required") {
		t.Fatalf("missing credential error=%v", err)
	}
	if state := readTestState(t, config.StateDir); state.ReportStatus != reportPending {
		t.Fatal("credential error discarded pending report")
	}
	config.ResetOnboarding = true
	if err := runOnboarding(context.Background(), logger, config, noBootstrap, noReport); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(config.StateDir, onboardingStateFile)); !os.IsNotExist(err) {
		t.Fatalf("reset retained pending report: %v", err)
	}
}

func TestOnboardingAcknowledgementWriteFailureDoesNotReprovision(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	config := testBootstrapConfig(t)
	logger := tmpTestLogger(new(bytes.Buffer))
	calls, reports := 0, 0
	bootstrap := func(context.Context) (sztpclient.CompletionReport, error) {
		calls++
		return testCompletionReport("serial"), nil
	}
	err := runOnboarding(context.Background(), logger, config, bootstrap,
		func(context.Context, sztpclient.CompletionReport) error {
			reports++
			return os.Chmod(config.StateDir, 0500)
		})
	if chmodErr := os.Chmod(config.StateDir, 0700); chmodErr != nil {
		t.Fatal(chmodErr)
	}
	if err == nil || calls != 1 || reports != 1 {
		t.Fatalf("ack write: calls=%d reports=%d error=%v", calls, reports, err)
	}
	if state := readTestState(t, config.StateDir); state.ReportStatus != reportPending {
		t.Fatal("failed acknowledgement destroyed pending report")
	}
	err = runOnboarding(context.Background(), logger, config, bootstrap,
		func(context.Context, sztpclient.CompletionReport) error { reports++; return nil })
	if err != nil || calls != 1 || reports != 2 {
		t.Fatalf("restart: calls=%d reports=%d error=%v", calls, reports, err)
	}
}

func TestOnboardingReportCancellationKeepsLocalCompletion(t *testing.T) {
	config := testBootstrapConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := runOnboarding(ctx, tmpTestLogger(new(bytes.Buffer)), config,
		func(context.Context) (sztpclient.CompletionReport, error) { return testCompletionReport("serial"), nil },
		func(ctx context.Context, _ sztpclient.CompletionReport) error {
			cancel()
			return ctx.Err()
		})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error=%v", err)
	}
	if state := readTestState(t, config.StateDir); state.ReportStatus != reportPending {
		t.Fatal("cancellation lost pending report")
	}
}

func TestOnboardingStoreLifecycle(t *testing.T) {
	store := testOnboardingStore(t)
	state, err := store.Read()
	if err != nil || state != nil {
		t.Fatalf("initial state=%v, error=%v", state, err)
	}
	if err := store.CheckWritable(); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(legacyState("device-42")); err != nil {
		t.Fatal(err)
	}
	state, err = store.Read()
	if err != nil || state == nil {
		t.Fatalf("saved state=%v, error=%v", state, err)
	}
	if state.Version != 1 || state.SerialNumber != "device-42" || state.CompletedAt.IsZero() || state.CompletedAt.Location() != time.UTC {
		t.Fatalf("unexpected completion record: %+v", state)
	}
	for _, tc := range []struct {
		path string
		mode os.FileMode
	}{
		{store.dir, 0700},
		{filepath.Join(store.dir, onboardingStateFile), 0600},
		{filepath.Join(store.dir, ".onboarding.lock"), 0600},
	} {
		info, err := os.Stat(tc.path)
		if err != nil || info.Mode().Perm() != tc.mode {
			t.Fatalf("permissions for %s: %v, error=%v", tc.path, info, err)
		}
	}
	entries, err := os.ReadDir(store.dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("unexpected retained files: %v (%v)", entries, err)
	}
	if err := store.Save(legacyState("updated-serial")); err != nil {
		t.Fatal(err)
	}
	state, err = store.Read()
	if err != nil || state.SerialNumber != "updated-serial" {
		t.Fatalf("replacement=%v (%v)", state, err)
	}
}

func TestOnboardingStateValidation(t *testing.T) {
	for _, tc := range []struct{ name, data string }{
		{"empty", ""},
		{"partial JSON", `{"version":1`},
		{"missing time", `{"version":1}`},
		{"missing version", `{"completed_at":"2026-10-05T12:00:00Z"}`},
		{"unknown version", `{"version":2,"completed_at":"2026-10-05T12:00:00Z"}`},
		{"invalid time", `{"version":1,"completed_at":"yesterday"}`},
		{"zero time", `{"version":1,"completed_at":"0001-01-01T00:00:00Z"}`},
		{"null", `null`},
		{"trailing JSON", `{"version":1,"completed_at":"2026-10-05T12:00:00Z"} {}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := testOnboardingStore(t)
			path := filepath.Join(store.dir, onboardingStateFile)
			if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Read(); err == nil {
				t.Fatal("invalid state accepted")
			}
			if err := store.Reset(); err != nil {
				t.Fatal(err)
			}
			if err := store.Reset(); err != nil {
				t.Fatalf("repeated reset failed: %v", err)
			}
			if state, err := store.Read(); err != nil || state != nil {
				t.Fatalf("reset state=%v (%v)", state, err)
			}
		})
	}
}

func TestOnboardingStatePublicationFailure(t *testing.T) {
	store := testOnboardingStore(t)
	path := filepath.Join(store.dir, onboardingStateFile)
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(); err == nil {
		t.Fatal("directory accepted as a completion record")
	}
	if err := store.Save(legacyState("device")); err == nil {
		t.Fatal("rename over a directory succeeded")
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		t.Fatalf("failed publication changed existing path: %v (%v)", info, err)
	}
	entries, err := os.ReadDir(store.dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("failed publication left temporary files: %v (%v)", entries, err)
	}
}

func TestOnboardingIgnoresUnpublishedTemporaryState(t *testing.T) {
	store := testOnboardingStore(t)
	if err := os.WriteFile(filepath.Join(store.dir, ".onboarded-interrupted.tmp"), []byte(`{"version":1,"completed_at":"2026-10-05T12:00:00Z"}`), 0600); err != nil {
		t.Fatal(err)
	}
	state, err := store.Read()
	if err != nil || state != nil {
		t.Fatalf("unpublished record was accepted: %v (%v)", state, err)
	}
}

func TestOnboardingRejectsDanglingStateSymlink(t *testing.T) {
	store := testOnboardingStore(t)
	if err := os.Symlink(filepath.Join(store.dir, "missing"), filepath.Join(store.dir, onboardingStateFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(); err == nil {
		t.Fatal("dangling completion symlink allowed a new bootstrap")
	}
	if err := store.Reset(); err != nil {
		t.Fatal(err)
	}
}

func TestOnboardingDirectoryErrors(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := openOnboardingStore(path); err == nil {
		t.Fatal("file accepted as state directory")
	}
	store := testOnboardingStore(t)
	if err := os.Chmod(store.dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(store.dir, 0700) })
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	if err := store.CheckWritable(); err == nil {
		t.Fatal("read-only state directory passed write probe")
	}
}

// Hold the real OS lock in a separate process until killed by the parent.
func TestOnboardingLockHelper(t *testing.T) {
	dir := os.Getenv("SZTP_TEST_LOCK_DIR")
	if dir == "" {
		t.Skip("subprocess helper")
	}
	store, err := openOnboardingStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, _ = os.Stdout.WriteString("locked\n")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func TestOnboardingLockAcrossProcessesAndCrash(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	cmd := exec.Command(os.Args[0], "-test.run=^TestOnboardingLockHelper$")
	cmd.Env = append(os.Environ(), "SZTP_TEST_LOCK_DIR="+dir)
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	waited := false
	t.Cleanup(func() {
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(output).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if line != "locked\n" {
			t.Fatalf("lock helper did not start: %q", line)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("lock helper timed out")
	}
	for _, reset := range []bool{false, true} {
		config := &Config{StateDir: dir, ResetOnboarding: reset}
		err := runTestOnboarding(tmpTestLogger(new(bytes.Buffer)), config, func() (string, error) { t.Fatal("bootstrap ran while locked"); return "", nil })
		if err == nil || !strings.Contains(err.Error(), "already running") {
			t.Fatalf("reset=%v, lock error=%v", reset, err)
		}
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	waited = true
	store, err := openOnboardingStore(dir)
	if err != nil {
		t.Fatalf("crashed process retained lock: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}
