// SPDX-FileCopyrightText: 2022-2023 Intel Corporation
// SPDX-FileCopyrightText: 2022 Dell Inc, or its subsidiaries
// SPDX-FileCopyrightText: 2022 Red Hat
// SPDX-FileCopyrightText: 2025 Anssi Söderena <https://github.com/asode>
// SPDX-FileCopyrightText: 2025 Sandip Ghimire <https://github.com/sandip-ghimire>
// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode> (modifications)
// SPDX-License-Identifier: Apache-2.0

package sztpclient

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jaypipes/ghw"
	"gopkg.in/ini.v1"
)

// DeviceInformation contains both the physical identity and the identity used
// for bootstrap authentication. Missing optional fields are empty.
type DeviceInformation struct {
	HardwareSerialNumber string
	HardwareSerialSource string
	SerialNumber         string
	SerialSource         string
	HardwareModel        string
	OSName               string
	OSVersion            string
}

// DiscoverDeviceInformation reads local hardware and OS information without
// initializing a bootstrap client. A serial override affects authentication
// only; the physical serial number is still discovered and returned.
// Partial information is returned even when no usable serial number is found.
func DiscoverDeviceInformation(logger *slog.Logger, serialOverride string) (DeviceInformation, error) {
	var info DeviceInformation
	hardwareLogger := logger.With("component", "ghw")
	acceptHardware := func(source, serial, model string) {
		if info.HardwareSerialNumber == "" && usableHardwareValue(serial) {
			info.HardwareSerialNumber = strings.TrimSpace(serial)
			info.HardwareSerialSource = source
		}
		if info.HardwareModel == "" && usableHardwareValue(model) {
			info.HardwareModel = strings.TrimSpace(model)
		}
	}

	if product, err := ghw.Product(ghw.WithLogger(hardwareLogger)); err != nil {
		logger.Debug("Error getting device product info", "error", err)
	} else {
		acceptHardware("product", product.SerialNumber, product.Name)
	}
	if board, err := ghw.Baseboard(ghw.WithLogger(hardwareLogger)); err != nil {
		logger.Debug("Error getting baseboard info", "error", err)
	} else {
		acceptHardware("baseboard", board.SerialNumber, board.Product)
	}
	if chassis, err := ghw.Chassis(ghw.WithLogger(hardwareLogger)); err != nil {
		logger.Debug("Error getting chassis info", "error", err)
	} else {
		acceptHardware("chassis", chassis.SerialNumber, chassis.Version)
	}

	if cfg, err := ini.Load("/etc/os-release"); err != nil {
		logger.Debug("Error loading os-release file", "error", err)
	} else {
		info.OSName = cfg.Section("").Key("NAME").String()
		info.OSVersion = cfg.Section("").Key("VERSION").String()
	}

	info.SerialNumber = info.HardwareSerialNumber
	info.SerialSource = info.HardwareSerialSource
	if serialOverride != "" {
		info.SerialNumber = serialOverride
		info.SerialSource = "override"
	}
	if info.SerialNumber == "" {
		return info, fmt.Errorf("could not determine the device serial number")
	}
	logger.Debug("Using device serial number", "source", info.SerialSource, "serial_number", info.SerialNumber)
	return info, nil
}

func usableHardwareValue(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && value != "0" && !strings.EqualFold(value, "unknown")
}

func (a *Client) getDeviceInformation() error {
	info, err := DiscoverDeviceInformation(a.Logger, a.SerialNumber)
	if err != nil {
		return err
	}
	a.SerialNumber = info.SerialNumber
	var input InputJSON
	input.IetfSztpBootstrapServerInput.HwModel = info.HardwareModel
	input.IetfSztpBootstrapServerInput.OsName = info.OSName
	input.IetfSztpBootstrapServerInput.OsVersion = info.OSVersion
	// Nonce remains empty until nonce generation is implemented.
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("error marshalling device information: %w", err)
	}
	a.InputJSONContent = string(inputJSON)
	return nil
}
