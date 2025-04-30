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
	"encoding/json"
	"fmt"
	"os"

	"github.com/go-ini/ini"
	"github.com/jaypipes/ghw"
)

// DiscoverDeviceInformation discovers the device information and sets the serial number, hardware model, OS name, OS version, and nonce.
// The serial number is set to the user-provided serial number if it is provided.
// The hardware model is set to the discovered product name, baseboard product, or chassis version if available.
// The OS name and version are set to the values in the /etc/os-release file if available.
func (a *Client) getDeviceInformation() error {
	var err error
	var serialNumber string
	var osName string
	var osVersion string
	var hwModel string
	var nonce string = "" // TODO

	// Use the user-provided serial number if it is set
	if a.SerialNumber != "" {
		a.Logger.Debug("Using user-provided serial number: ", a.SerialNumber)
		serialNumber = a.SerialNumber
	}

	// The ghw.Product() function returns a ghw.ProductInfo struct that contains information about the host computer's hardware product line.
	product, err := ghw.Product()
	if err != nil {
		a.Logger.Debug("Error getting device product info: ", err)
	} else {
		// Use the discovered product serial number if the user did not provide one
		if serialNumber == "" {
			if product.SerialNumber != "" && product.SerialNumber != "0" {
				a.Logger.Debug("Using discovered product serial number: ", product.SerialNumber)
				serialNumber = product.SerialNumber
			}
		}
		// Use the discovered product name if not already set
		if hwModel == "" {
			if product.Name != "" && product.Name != "0" {
				hwModel = product.Name
			}
		}
	}

	// The ghw.Baseboard() function returns a ghw.BaseboardInfo struct that contains information about the host computer's hardware baseboard.
	baseboard, err := ghw.Baseboard()
	if err != nil {
		a.Logger.Debug("Error getting baseboard info: ", err)
	} else {
		// Use the discovered baseboard product name if the user did not provide one
		if serialNumber == "" {
			if baseboard.SerialNumber != "" && baseboard.SerialNumber != "0" {
				a.Logger.Debug("Using discovered baseboard serial number: ", baseboard.SerialNumber)
				serialNumber = baseboard.SerialNumber
			}
		}
		// Use the discovered baseboard product name if not already set
		if hwModel == "" {
			if baseboard.Product != "" && baseboard.Product != "0" {
				a.Logger.Debug("Using discovered baseboard product name: ", baseboard.Product)
				hwModel = baseboard.Product
			}
		}
	}

	// The ghw.Chassis() function returns a ghw.ChassisInfo struct that contains information about the host computer's hardware chassis.
	chassis, err := ghw.Chassis()
	if err != nil {
		a.Logger.Debug("Error getting chassis info: ", err)
	} else {
		// Use the discovered chassis serial number if the user did not provide one
		if serialNumber == "" {
			if chassis.SerialNumber != "" && chassis.SerialNumber != "0" {
				a.Logger.Debug("Using discovered chassis serial number: ", chassis.SerialNumber)
				serialNumber = chassis.SerialNumber
			}
		}
		// Use the discovered chassis product version if not already set
		if hwModel == "" {
			if chassis.Version != "" && chassis.Version != "0" {
				a.Logger.Debug("Using discovered chassis product version: ", chassis.Version)
				hwModel = chassis.Version
			}
		}
	}

	// Try to load the os-release file and get the OS name and version
	var osReleaseFile string

	if _, err := os.Stat("/etc/os-release"); err == nil {
		osReleaseFile = "/etc/os-release"
	}

	cfg, err := ini.Load(osReleaseFile)
	if err != nil {
		a.Logger.Debug("Error loading os-release file: ", err)
	} else {
		// Use the discovered OS name if not set
		if osName == "" {
			a.Logger.Debug("Using discovered OS name: ", cfg.Section("").Key("NAME").String())
			osName = cfg.Section("").Key("NAME").String()
		}
		// Use the discovered OS version if not set
		if osVersion == "" {
			a.Logger.Debug("Using discovered OS version: ", cfg.Section("").Key("VERSION").String())
			osVersion = cfg.Section("").Key("VERSION").String()
		}
	}

	// If the serial number is still empty, return an error
	if serialNumber == "" {
		return fmt.Errorf("could not determine the device serial number")
	}

	// Set serial number
	a.SerialNumber = serialNumber

	// Set Device Information
	var input InputJSON
	input.IetfSztpBootstrapServerInput.HwModel = hwModel
	input.IetfSztpBootstrapServerInput.OsName = osName
	input.IetfSztpBootstrapServerInput.OsVersion = osVersion
	input.IetfSztpBootstrapServerInput.Nonce = nonce // TODO
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("error marshalling device information: %v", err)
	}
	a.InputJSONContent = string(inputJSON)

	return nil
}
