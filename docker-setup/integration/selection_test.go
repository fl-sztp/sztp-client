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
	"strings"
	"testing"
)

func TestCaseSelection(t *testing.T) {
	catalog := caseCatalog()
	names := make(map[string]bool)
	slow := 0
	for _, c := range catalog {
		if names[c.name] {
			t.Fatalf("duplicate case %q", c.name)
		}
		names[c.name] = true
		if c.slow {
			slow++
		}
	}
	if len(catalog) != 76 || slow != 3 {
		t.Fatalf("catalog has %d cases, %d slow", len(catalog), slow)
	}
	for _, tc := range []struct {
		name, selected string
		quick          bool
		count          int
		errorText      string
	}{
		{name: "full", count: 76},
		{name: "quick", quick: true, count: 73},
		{name: "exact", selected: "silent_v4_timeout_then_retry", count: 1},
		{name: "unknown", selected: "v6_.*", errorText: "valid cases:"},
		{name: "quick_slow", selected: "silent_v4_timeout_then_retry", quick: true, errorText: "cannot run with --quick"},
		{name: "quick_fast", selected: "v6_text_hostname", quick: true, count: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := selectCases(tc.selected, tc.quick)
			if tc.errorText != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errorText) {
					t.Fatalf("selection error %v, want %q", err, tc.errorText)
				}
				return
			}
			if err != nil || len(got) != tc.count {
				t.Fatalf("selection count %d error %v, want %d", len(got), err, tc.count)
			}
			for _, c := range got {
				if tc.selected != "" && c.name != tc.selected {
					t.Fatalf("selected %q, want %q", c.name, tc.selected)
				}
				if tc.quick && c.slow {
					t.Fatalf("quick selected slow case %q", c.name)
				}
			}
		})
	}
}
