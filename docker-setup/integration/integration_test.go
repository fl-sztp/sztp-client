//go:build linux && dhcp_integration

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
