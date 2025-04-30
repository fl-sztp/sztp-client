/*
SPDX-License-Identifier: Apache-2.0
Copyright (C) 2022-2023 Intel Corporation
Copyright (c) 2022 Dell Inc, or its subsidiaries
Copyright (C) 2022 Red Hat
Copyright (C) 2025 Anssi Söderena, Sandip Ghimire

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

package sztpclient

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
)

func (a *Client) getConfigurationFile() error {
	a.Logger.Info("Starting the Copy Configuration.")
	a.postProgress(ProgressTypeConfigInitiated, "Configuration Initiated")

	// Copy the configuration file to the device
	file, err := os.Create(filepath.Join(a.TmpFolder, a.BootstrapServerOnboardingInfo.IetfSztpConveyedInfoOnboardingInformation.InfoTimestampReference+"-configuration"))
	if err != nil {
		a.Logger.Error("Error creating the configuration file: ", err)
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			a.Logger.Error("Error when closing:", err)
		}
	}()

	plainTest, _ := base64.StdEncoding.DecodeString(a.BootstrapServerOnboardingInfo.IetfSztpConveyedInfoOnboardingInformation.Configuration)
	_, err = file.WriteString(string(plainTest))
	if err != nil {
		a.Logger.Error("Error writing the configuration file: ", err)
		return err
	}

	err = os.Chmod(filepath.Join(a.TmpFolder, a.BootstrapServerOnboardingInfo.IetfSztpConveyedInfoOnboardingInformation.InfoTimestampReference+"-configuration"), 0744)
	if err != nil {
		a.Logger.Error("Error changing the configuration file permission: ", err)
		return err
	}
	a.Logger.Info("Configuration file copied successfully")
	a.postProgress(ProgressTypeConfigComplete, "Configuration Complete")
	return nil
}

func (a *Client) runConfiguration(typeOf string) error {
	var script, scriptName string
	var reportStart, reportEnd, scriptError, scriptInfo ProgressType

	scriptInfo = ProgressTypeInformational

	switch typeOf {
	case "post":
		script = a.BootstrapServerOnboardingInfo.IetfSztpConveyedInfoOnboardingInformation.PostConfigurationScript
		scriptName = "post"
		reportStart = ProgressTypePostScriptInitiated
		reportEnd = ProgressTypePostScriptComplete
		scriptError = ProgressTypePostScriptError
	default: // pre or default
		script = a.BootstrapServerOnboardingInfo.IetfSztpConveyedInfoOnboardingInformation.PreConfigurationScript
		scriptName = "pre"
		reportStart = ProgressTypePreScriptInitiated
		reportEnd = ProgressTypePreScriptComplete
		scriptError = ProgressTypePreScriptError
	}
	a.Logger.Info("Starting the " + scriptName + "-" + "configuration.sh")
	a.postProgress(reportStart, "Report starting")

	file, err := os.Create(filepath.Join(a.TmpFolder, a.BootstrapServerOnboardingInfo.IetfSztpConveyedInfoOnboardingInformation.InfoTimestampReference+"-"+scriptName+"-"+"configuration.sh"))
	if err != nil {
		a.Logger.Error("Error creating the "+scriptName+"-configuration script: ", err)
		a.postProgress(scriptError, "Error creating the "+scriptName+"-configuration script")
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			a.Logger.Error("Error when closing:", err)
		}
	}()

	plainTest, _ := base64.StdEncoding.DecodeString(script)
	_, err = file.WriteString(string(plainTest))
	if err != nil {
		a.Logger.Error("Error writing the "+scriptName+"-configuration script: ", err)
		a.postProgress(scriptError, "Error writing the "+scriptName+"-configuration script")
		return err
	}
	// nolint:gosec
	err = os.Chmod(filepath.Join(a.TmpFolder, a.BootstrapServerOnboardingInfo.IetfSztpConveyedInfoOnboardingInformation.InfoTimestampReference+"-"+scriptName+"-"+"configuration.sh"), 0755)
	if err != nil {
		a.Logger.Error("Error changing the "+scriptName+"-configuration script permission: ", err)
		a.postProgress(scriptError, "Error changing the "+scriptName+"-configuration script permission")
		return err
	}
	a.Logger.Info(scriptName + "-configuration script created successfully")

	scriptPath := filepath.Join(a.TmpFolder, a.BootstrapServerOnboardingInfo.IetfSztpConveyedInfoOnboardingInformation.InfoTimestampReference+"-"+scriptName+"-"+"configuration.sh")
	// Check if /bin/bash is available
	bashPath, err := exec.LookPath("bash")
	var cmd *exec.Cmd

	if err == nil {
		// Use /bin/bash if available
		a.Logger.Debug("Using /bin/bash to run the " + scriptName + "-configuration script.")
		cmd = exec.Command(bashPath, scriptPath)
	} else {
		// Fallback to /bin/sh
		a.Logger.Debug("/bin/bash not found, falling back to /bin/sh.")
		cmd = exec.Command("/bin/sh", scriptPath)
	}

	out, err := cmd.Output()
	a.Logger.Debug("Output of the " + scriptName + "-configuration script: " + string(out))
	a.postProgress(scriptInfo, "Script output: "+string(out))
	if err != nil {
		a.Logger.Error("Error running the " + scriptName + "-configuration script: " + err.Error())
		a.postProgress(scriptError, "Error running the "+scriptName+"-configuration script: "+err.Error())
		return err
	}
	a.postProgress(reportEnd, "Report end")
	a.Logger.Info(scriptName + "-Configuration script executed successfully")
	return nil
}
