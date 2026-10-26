// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// prepareTmpFolder creates a private workspace, even with a configured parent.
// The caller must register it before writing artifacts, then clean it after use.
func prepareTmpFolder(logger *slog.Logger, path string) (string, error) {
	var configuredErr error
	if path != "" {
		configuredErr = prepareConfiguredTmpFolder(path)
		if configuredErr == nil {
			var folder string
			folder, configuredErr = createWorkspace(path)
			if configuredErr == nil {
				return folder, nil
			}
		}
		logger.Warn("Temporary folder is unusable; falling back to the default temporary folder", "path", path, "error", configuredErr)
	}

	folder, err := createWorkspace(os.TempDir())
	if err != nil {
		defaultErr := fmt.Errorf("default temporary folder: %w", err)
		if configuredErr != nil {
			return "", errors.Join(fmt.Errorf("configured temporary folder %q: %w", path, configuredErr), defaultErr)
		}
		return "", defaultErr
	}
	return folder, nil
}

func createWorkspace(parent string) (string, error) {
	parent, err := filepath.Abs(parent)
	if err != nil {
		return "", err
	}
	parent, err = filepath.EvalSymlinks(parent)
	if err != nil {
		return "", err
	}
	pattern := "sztp-client-" + time.Now().UTC().Format("20060102T150405Z") + "-"
	return os.MkdirTemp(parent, pattern)
}

func prepareConfiguredTmpFolder(path string) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		if err := os.Mkdir(path, 0755); err != nil {
			return fmt.Errorf("create temporary folder: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("inspect temporary folder: %w", err)
	} else if !info.IsDir() {
		return fmt.Errorf("temporary path %q is not a directory", path)
	}
	return checkTmpFolderWritable(path)
}

func checkTmpFolderWritable(path string) error {
	file, err := os.CreateTemp(path, ".sztp-client-probe-")
	if err != nil {
		return fmt.Errorf("create temporary write probe: %w", err)
	}

	_, writeErr := file.WriteString("sztp-client\n")
	closeErr := file.Close()
	removeErr := os.Remove(file.Name())
	if err := errors.Join(writeErr, closeErr, removeErr); err != nil {
		return fmt.Errorf("temporary folder %q failed write probe: %w", path, err)
	}
	return nil
}
