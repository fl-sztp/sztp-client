//go:build linux

/*
SPDX-License-Identifier: Apache-2.0
Copyright (C) 2026 Anssi Söderena

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
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestDeviceInformationLinux(t *testing.T) {
	// os-release defines shell assignments. Use the shell as an independent
	// reference for the INI reader, including quotes and optional VERSION.
	wantOSName, wantOSVersion := "", ""
	if _, err := os.Stat("/etc/os-release"); err == nil {
		output, err := exec.Command("/bin/sh", "-c", `NAME= VERSION=; . /etc/os-release; printf '%s\n%s' "$NAME" "$VERSION"`).Output()
		if err != nil {
			t.Fatal(err)
		}
		wantOSName, wantOSVersion, _ = strings.Cut(string(output), "\n")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}

	const dmi = "sys/class/dmi/id/"
	const deviceTree = "sys/firmware/devicetree/base/"
	for _, tt := range []struct {
		name, serial, wantSerial, wantModel string
		files                               map[string]string
	}{
		{
			name:       "DMI product",
			wantSerial: "product-serial", wantModel: "Product Model",
			files: map[string]string{dmi + "product_serial": "product-serial\n", dmi + "product_name": "Product Model\n"},
		},
		{
			name:   "serial override",
			serial: "user-serial", wantSerial: "user-serial", wantModel: "Product Model",
			files: map[string]string{dmi + "product_serial": "product-serial\n", dmi + "product_name": "Product Model\n"},
		},
		{
			name:       "baseboard fallback",
			wantSerial: "board-serial", wantModel: "Board Model",
			files: map[string]string{
				dmi + "product_serial": "0\n", dmi + "product_name": "0\n",
				dmi + "board_serial": "board-serial\n", dmi + "board_name": "Board Model\n",
			},
		},
		{
			name:       "chassis fallback",
			wantSerial: "chassis-serial", wantModel: "Chassis Model",
			files: map[string]string{
				dmi + "product_serial": "0\n", dmi + "product_name": "0\n",
				dmi + "board_serial": "0\n", dmi + "board_name": "0\n",
				dmi + "chassis_serial": "chassis-serial\n", dmi + "chassis_version": "Chassis Model\n",
			},
		},
		{
			name:       "DeviceTree without DMI",
			wantSerial: "device-tree-serial", wantModel: "DeviceTree Model",
			files: map[string]string{deviceTree + "serial-number": "device-tree-serial\x00", deviceTree + "model": "DeviceTree Model\x00"},
		},
		{
			name:   "serial override without firmware",
			serial: "user-serial", wantSerial: "user-serial", wantModel: "unknown",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("GHW_CHROOT", root)
			t.Setenv("GHW_DISABLE_TOOLS", "1")
			t.Setenv("GHW_DISABLE_WARNINGS", "1")
			for name, content := range tt.files {
				path := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			logger := logrus.New()
			logger.SetOutput(io.Discard)
			client := &Client{Logger: logger, SerialNumber: tt.serial}
			if err := client.getDeviceInformation(); err != nil {
				t.Fatal(err)
			}
			if client.SerialNumber != tt.wantSerial {
				t.Errorf("serial = %q, want %q", client.SerialNumber, tt.wantSerial)
			}
			var payload InputJSON
			if err := json.Unmarshal([]byte(client.InputJSONContent), &payload); err != nil {
				t.Fatal(err)
			}
			got := payload.IetfSztpBootstrapServerInput
			if got.HwModel != tt.wantModel || got.OsName != wantOSName || got.OsVersion != wantOSVersion {
				t.Errorf("device payload = %+v, want model %q, OS %q, version %q", got, tt.wantModel, wantOSName, wantOSVersion)
			}
		})
	}
}
