//go:build !linux
// +build !linux

// SPDX-FileCopyrightText: 2025 Anssi Söderena <https://github.com/asode>
// SPDX-FileCopyrightText: 2025 Sandip Ghimire <https://github.com/sandip-ghimire>
// SPDX-License-Identifier: Apache-2.0

package dhcpclient

import (
	"fmt"
	"log/slog"
)

func GetServerFromDHCP6(logger *slog.Logger) (string, error) {
	// TODO Support other OS
	return "", fmt.Errorf("OS not supported")

}
