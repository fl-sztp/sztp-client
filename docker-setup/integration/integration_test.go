//go:build linux && dhcp_integration

// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"context"
	"testing"
	"time"
)

// TestDockerDHCP runs serially with a fresh client and fixture reset per case.
func TestDockerDHCP(t *testing.T) {
	cases, err := selectCases(*selectedCase, testing.Short())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	driver := newDriver(t, ctx)
	for _, c := range cases {
		if err := ctx.Err(); err != nil {
			t.Fatalf("integration suite deadline: %v", err)
		}
		t.Run(c.name, func(t *testing.T) { driver.run(t, ctx, c.integrationCase) })
	}
}
