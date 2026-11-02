//go:build linux && dhcp_integration

// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

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
	if len(catalog) != 89 || slow != 4 {
		t.Fatalf("catalog has %d cases, %d slow", len(catalog), slow)
	}
	for _, tc := range []struct {
		name, selected string
		quick          bool
		count          int
		errorText      string
	}{
		{name: "full", count: 89},
		{name: "quick", quick: true, count: 85},
		{name: "exact", selected: "silent_v6_timeout_then_retry", count: 1},
		{name: "unknown", selected: "v6_.*", errorText: "valid cases:"},
		{name: "quick_slow", selected: "silent_v6_timeout_then_retry", quick: true, errorText: "cannot run with --quick"},
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
