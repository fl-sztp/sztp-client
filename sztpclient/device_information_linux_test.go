//go:build linux

// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package sztpclient

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"fl-sztp-client/internal/logging"
)

func TestHardwareDiscoveryUsesInjectedLogger(t *testing.T) {
	t.Setenv("GHW_CHROOT", t.TempDir())
	t.Setenv("GHW_DISABLE_TOOLS", "1")
	// The explicitly supplied application logger controls output, even if old
	// ghw environment settings request a different level or format.
	t.Setenv("GHW_LOG_LEVEL", "error")
	t.Setenv("GHW_DISABLE_WARNINGS", "1")
	t.Setenv("GHW_LOG_LOGFMT", "1")
	for _, level := range []slog.Level{slog.LevelInfo, slog.LevelDebug} {
		t.Run(level.String(), func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: level}))
			info, err := DiscoverDeviceInformation(logger, "override")
			if err != nil || info.SerialNumber != "override" {
				t.Fatalf("identity discovery changed: %+v, %v", info, err)
			}
			warnings, debug := 0, 0
			decoder := json.NewDecoder(&output)
			for decoder.More() {
				var record map[string]any
				if err := decoder.Decode(&record); err != nil {
					t.Fatal(err)
				}
				if record["component"] != "ghw" {
					continue
				}
				if record["level"] == "WARN" {
					warnings++
				}
				if record["level"] == "DEBUG" {
					debug++
				}
			}
			if warnings == 0 || (debug > 0) != (level == slog.LevelDebug) {
				t.Fatalf("ghw ignored application logging: warnings=%d debug=%d", warnings, debug)
			}
		})
	}
}

func TestDiscoveredAndEffectiveDeviceInformation(t *testing.T) {
	const dmi = "sys/class/dmi/id/"
	const dt = "sys/firmware/devicetree/base/"
	for _, tc := range []struct {
		name, override, physical, source, effective, model string
		files                                              map[string]string
		wantErr                                            bool
	}{
		{name: "product with override", override: "configured", physical: "physical", source: "product", effective: "configured", model: "Model", files: map[string]string{dmi + "product_serial": "physical\n", dmi + "product_name": "Model\n"}},
		{name: "unknown product falls back to board", physical: "board", source: "baseboard", effective: "board", model: "Board", files: map[string]string{dmi + "product_serial": "unknown\n", dmi + "product_name": "unknown\n", dmi + "board_serial": "board\n", dmi + "board_name": "Board\n"}},
		{name: "unknown board falls back to chassis", physical: "chassis", source: "chassis", effective: "chassis", model: "Chassis", files: map[string]string{dmi + "product_serial": "0\n", dmi + "product_name": "0\n", dmi + "board_serial": "unknown\n", dmi + "board_name": "unknown\n", dmi + "chassis_serial": "chassis\n", dmi + "chassis_version": "Chassis\n"}},
		{name: "DeviceTree", physical: "dt-serial", source: "product", effective: "dt-serial", model: "DT Model", files: map[string]string{dt + "serial-number": "dt-serial\x00", dt + "model": "DT Model\x00"}},
		{name: "missing serial retains partial information", model: "Model", wantErr: true, files: map[string]string{dmi + "product_name": "Model\n"}},
		{name: "unknown never authenticates", wantErr: true, files: map[string]string{dmi + "product_serial": "unknown\n", dmi + "board_serial": "unknown\n", dmi + "chassis_serial": "unknown\n"}},
		{name: "override without firmware", override: "configured", effective: "configured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("GHW_CHROOT", root)
			t.Setenv("GHW_DISABLE_TOOLS", "1")
			for name, content := range tc.files {
				path := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			logger := logging.New(io.Discard, false)
			info, err := DiscoverDeviceInformation(logger, tc.override)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v", err)
			}
			if info.HardwareSerialNumber != tc.physical || info.HardwareSerialSource != tc.source || info.SerialNumber != tc.effective || info.HardwareModel != tc.model {
				t.Fatalf("unexpected information: %+v", info)
			}
			wantSource := tc.source
			if tc.override != "" {
				wantSource = "override"
			}
			if info.SerialSource != wantSource {
				t.Fatalf("serial source=%q, want %q", info.SerialSource, wantSource)
			}
		})
	}
}
