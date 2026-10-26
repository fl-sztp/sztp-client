// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const workspaceStateFile = "workspace.json"

type workspaceRecord struct {
	Path string `json:"path"`
}

func validateWorkspacePath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("workspace path must be absolute and clean")
	}
	name := filepath.Base(path)
	if !strings.HasPrefix(name, "sztp-client-") {
		return errors.New("workspace path lacks the private workspace prefix")
	}
	stamp, suffix, ok := strings.Cut(strings.TrimPrefix(name, "sztp-client-"), "-")
	if _, err := time.Parse("20060102T150405Z", stamp); err != nil || !ok || suffix == "" {
		return errors.New("invalid private workspace name")
	}
	// Never follow a replacement symlink in the workspace's parent path.
	parent := filepath.Dir(path)
	realParent, err := filepath.EvalSymlinks(parent)
	if os.IsNotExist(err) {
		return nil // A missing parent means the workspace is already gone.
	}
	if err != nil {
		return err
	}
	if realParent != parent {
		return errors.New("workspace parent contains a symlink")
	}
	return nil
}

func (s *onboardingStore) RegisterWorkspace(path string) error {
	if err := validateWorkspacePath(path); err != nil {
		return err
	}
	if _, err := os.Lstat(filepath.Join(s.dir, workspaceStateFile)); !os.IsNotExist(err) {
		return errors.New("previous workspace registration has not been cleared")
	}
	return s.writeJSON(workspaceStateFile, workspaceRecord{Path: path})
}

// CleanupWorkspace is called under the onboarding lock, including on starts
// that skip provisioning. Keep the registration if any cleanup step fails.
func (s *onboardingStore) CleanupWorkspace() error {
	path := filepath.Join(s.dir, workspaceStateFile)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("workspace registration is not a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var record workspaceRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return fmt.Errorf("read workspace registration: %w", err)
	}
	if err := validateWorkspacePath(record.Path); err != nil {
		return fmt.Errorf("unsafe workspace registration: %w", err)
	}
	stateDir, err := filepath.Abs(s.dir)
	if err != nil {
		return err
	}
	stateDir, err = filepath.EvalSymlinks(stateDir)
	if err != nil {
		return err
	}
	if stateDir == record.Path || strings.HasPrefix(stateDir, record.Path+string(os.PathSeparator)) {
		return errors.New("workspace registration contains the persistent state directory")
	}
	if err := os.RemoveAll(record.Path); err != nil {
		return fmt.Errorf("remove workspace %q: %w", record.Path, err)
	}
	// Persist the deletion before forgetting its path. Otherwise a power loss
	// could restore a workspace whose registration was already removed.
	if err := syncStateDirectory(filepath.Dir(record.Path)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("persist workspace removal: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove workspace registration: %w", err)
	}
	return syncStateDirectory(s.dir)
}

// A failed bootstrap must be cleaned before the next attempt. Retain the
// private workspace and its registration until the whole run finishes.
func clearWorkspace(path string) error {
	if err := validateWorkspacePath(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("workspace is no longer a directory")
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(path, entry.Name())); err != nil {
			return fmt.Errorf("clear workspace %q: %w", path, err)
		}
	}
	return nil
}
