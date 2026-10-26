// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"fl-sztp-client/sztpclient"
)

const (
	defaultStateDir     = "/var/lib/sztp-client"
	onboardingStateFile = "onboarded.json"
)

// A valid completion record means local provisioning succeeded. Pending reports
// reuse its serial; changing the configured serial never triggers provisioning.
type onboardingState struct {
	Version      int                          `json:"version"`
	CompletedAt  time.Time                    `json:"completed_at"`
	SerialNumber string                       `json:"serial_number"`
	Report       *sztpclient.CompletionReport `json:"report,omitempty"`
	ReportStatus string                       `json:"report_status,omitempty"`
}

const (
	reportPending      = "pending"
	reportAcknowledged = "acknowledged"
)

type onboardingStore struct {
	dir  string
	lock *os.File
}

func openOnboardingStore(dir string) (*onboardingStore, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create state directory %q: %w", dir, err)
	}
	file, err := os.OpenFile(filepath.Join(dir, ".onboarding.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open onboarding lock: %w", err)
	}
	if err := lockOnboarding(file); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return &onboardingStore{dir: dir, lock: file}, nil
}

func (s *onboardingStore) Close() error {
	// Closing releases the OS lock, including after a crash. Never unlink the
	// lock file: another process could otherwise lock a different inode.
	return s.lock.Close()
}

func (s *onboardingStore) Read() (*onboardingState, error) {
	path := filepath.Join(s.dir, onboardingStateFile)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect onboarding state %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("invalid onboarding state %q: not a regular file; use --reset-onboarding to clear it", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read onboarding state %q: %w", path, err)
	}
	var state onboardingState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("invalid onboarding state %q: %w; use --reset-onboarding to clear it", path, err)
	}
	if err := state.validate(); err != nil {
		return nil, fmt.Errorf("invalid onboarding state %q: %w; use --reset-onboarding to clear it", path, err)
	}
	return &state, nil
}

func (s onboardingState) validate() error {
	if (s.Version != 1 && s.Version != 2) || s.CompletedAt.IsZero() {
		return errors.New("unsupported version or missing completion time")
	}
	if s.Version == 1 {
		if s.Report != nil || s.ReportStatus != "" {
			return errors.New("legacy completion state contains report fields")
		}
		return nil
	}
	if s.Report == nil || s.Report.SerialNumber != s.SerialNumber ||
		(s.ReportStatus != reportPending && s.ReportStatus != reportAcknowledged) {
		return errors.New("missing or inconsistent completion report")
	}
	return s.Report.Validate()
}

func syncStateDirectory(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}

// Probe before provisioning so ordinary permission/storage failures do not
// occur only after scripts have already run. There is no temporary fallback.
func (s *onboardingStore) CheckWritable() error {
	file, err := os.CreateTemp(s.dir, ".onboarding-probe-")
	if err != nil {
		return fmt.Errorf("state directory is not writable: %w", err)
	}
	_, writeErr := file.WriteString("sztp-client\n")
	err = errors.Join(writeErr, file.Sync(), file.Close(), os.Remove(file.Name()), syncStateDirectory(s.dir))
	if err != nil {
		return fmt.Errorf("state directory failed write probe: %w", err)
	}
	return nil
}

func (s *onboardingStore) Save(state onboardingState) error {
	if err := state.validate(); err != nil {
		return err
	}
	return s.writeJSON(onboardingStateFile, state)
}

// Publish both completion state and workspace registration durably, under the
// onboarding lock. A crash can expose either the old record or the new one.
func (s *onboardingStore) writeJSON(name string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(s.dir, "."+name+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary onboarding state: %w", err)
	}
	// CreateTemp uses 0600. Cleanup also runs if rename or persistence fails.
	defer os.Remove(file.Name())
	_, writeErr := file.Write(append(data, '\n'))
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return fmt.Errorf("write onboarding state: %w", err)
	}
	if err := os.Rename(file.Name(), filepath.Join(s.dir, name)); err != nil {
		return fmt.Errorf("publish onboarding state: %w", err)
	}
	if err := syncStateDirectory(s.dir); err != nil {
		return fmt.Errorf("persist onboarding state directory: %w", err)
	}
	return nil
}

func (s *onboardingStore) Reset() error {
	if err := os.Remove(filepath.Join(s.dir, onboardingStateFile)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reset onboarding state: %w", err)
	}
	if err := syncStateDirectory(s.dir); err != nil {
		return fmt.Errorf("persist onboarding reset: %w", err)
	}
	return nil
}
