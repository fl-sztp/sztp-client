//go:build !linux && !darwin

// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"runtime"
)

func lockOnboarding(_ *os.File) error {
	return fmt.Errorf("onboarding state locking is not supported on %s", runtime.GOOS)
}
