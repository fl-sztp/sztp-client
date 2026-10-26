// SPDX-FileCopyrightText: 2022-2023 Intel Corporation
// SPDX-FileCopyrightText: 2022 Dell Inc, or its subsidiaries
// SPDX-FileCopyrightText: 2022 Red Hat
// SPDX-FileCopyrightText: 2025 Anssi Söderena <https://github.com/asode>
// SPDX-FileCopyrightText: 2025 Sandip Ghimire <https://github.com/sandip-ghimire>
// SPDX-License-Identifier: Apache-2.0

// Modified for sztp-client.

package sztpclient

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// prepareOnboardingArtifacts closes all three files before any script is run.
// The caller owns the workspace and removes it after saving local completion
// or before retrying a failed attempt.
func (a *Client) prepareOnboardingArtifacts(ctx context.Context) (string, error) {
	parent, err := filepath.Abs(a.TmpFolder)
	if err != nil {
		return "", fmt.Errorf("resolve temporary folder %q: %w", a.TmpFolder, err)
	}
	pattern := "onboarding-" + time.Now().UTC().Format("20060102T150405Z") + "-"
	onboardingDir, err := os.MkdirTemp(parent, pattern)
	if err != nil {
		return "", fmt.Errorf("create onboarding folder under %q: %w", parent, err)
	}
	a.Logger.Info("Using onboarding folder", "path", onboardingDir)
	a.postProgress(ctx, ProgressTypeConfigInitiated, "Configuration Initiated")

	info := a.BootstrapServerOnboardingInfo.IetfSztpConveyedInfoOnboardingInformation
	artifacts := []struct {
		name      string
		encoded   string
		mode      os.FileMode
		errorType ProgressType
		data      []byte
	}{
		{name: "configuration", encoded: info.Configuration, mode: 0744, errorType: ProgressTypeConfigError},
		{name: "pre-configuration.sh", encoded: info.PreConfigurationScript, mode: 0755, errorType: ProgressTypePreScriptError},
		{name: "post-configuration.sh", encoded: info.PostConfigurationScript, mode: 0755, errorType: ProgressTypePostScriptError},
	}
	// Reject a malformed payload before writing any of the files.
	for i := range artifacts {
		artifact := &artifacts[i]
		artifact.data, err = base64.StdEncoding.DecodeString(artifact.encoded)
		if err != nil {
			err = fmt.Errorf("decode onboarding file %q: %w", filepath.Join(onboardingDir, artifact.name), err)
			a.postProgress(ctx, artifact.errorType, err.Error())
			return "", err
		}
	}
	for _, artifact := range artifacts {
		path := filepath.Join(onboardingDir, artifact.name)
		// WriteFile also returns close errors, so execution cannot start with
		// an unclosed or incompletely written file.
		err = os.WriteFile(path, artifact.data, artifact.mode)
		if err == nil {
			// Preserve the existing file permissions independently of umask.
			err = os.Chmod(path, artifact.mode)
		}
		if err != nil {
			err = fmt.Errorf("prepare onboarding file %q: %w", path, err)
			a.postProgress(ctx, artifact.errorType, err.Error())
			return "", err
		}
	}
	a.Logger.Info("Onboarding files prepared successfully")
	a.postProgress(ctx, ProgressTypeConfigComplete, "Configuration Complete")
	return onboardingDir, nil
}

func (a *Client) runConfiguration(ctx context.Context, typeOf, onboardingDir string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var scriptName string
	var reportStart, reportEnd, scriptError, scriptInfo ProgressType

	scriptInfo = ProgressTypeInformational

	switch typeOf {
	case "post":
		scriptName = "post"
		reportStart = ProgressTypePostScriptInitiated
		reportEnd = ProgressTypePostScriptComplete
		scriptError = ProgressTypePostScriptError
	default: // pre or default
		scriptName = "pre"
		reportStart = ProgressTypePreScriptInitiated
		reportEnd = ProgressTypePreScriptComplete
		scriptError = ProgressTypePreScriptError
	}
	a.Logger.Info("Starting configuration script", "phase", scriptName)
	a.postProgress(ctx, reportStart, "Report starting")

	scriptPath := filepath.Join(onboardingDir, scriptName+"-configuration.sh")
	// Check if /bin/bash is available
	bashPath, err := exec.LookPath("bash")
	var cmd *exec.Cmd

	if err == nil {
		// Use /bin/bash if available
		a.Logger.Debug("Using shell for configuration script", "phase", scriptName, "shell", bashPath)
		cmd = exec.Command(bashPath, scriptPath)
	} else {
		// Fallback to /bin/sh
		a.Logger.Debug("Bash not found; falling back to sh", "phase", scriptName, "shell", "sh")
		cmd = exec.Command("/bin/sh", scriptPath)
	}
	cmd.Dir = onboardingDir

	stdout := newScriptLogWriter(a.Logger, scriptName, "stdout")
	stderr := newScriptLogWriter(a.Logger, scriptName, "stderr")
	tail := &scriptOutputTail{}
	cmd.Stdout = io.MultiWriter(stdout, tail)
	cmd.Stderr = stderr
	err = cmd.Run()
	stdout.Flush()
	stderr.Flush()
	a.postProgress(ctx, scriptInfo, "Script output: "+tail.String())
	if err != nil {
		a.postProgress(ctx, scriptError, "Error running the "+scriptName+"-configuration script: "+err.Error())
		return err
	}
	a.postProgress(ctx, reportEnd, "Report end")
	a.Logger.Info("Configuration script executed successfully", "phase", scriptName)
	return nil
}
