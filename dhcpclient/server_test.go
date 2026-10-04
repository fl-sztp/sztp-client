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

package dhcpclient

import (
	"io"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

type optionLogHook struct {
	entries []*logrus.Entry
}

func (h *optionLogHook) Levels() []logrus.Level { return logrus.AllLevels }
func (h *optionLogHook) Fire(entry *logrus.Entry) error {
	h.entries = append(h.entries, entry)
	return nil
}

func TestServerFromOption(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		present bool
		want    string
	}{
		{"full RPC URL", "https://bootstrap:8080/restconf/operations/ietf-sztp-bootstrap-server:get-bootstrapping-data", true, "https://bootstrap:8080"},
		{"bare host and port", "bootstrap:8080", true, "https://bootstrap:8080"},
		{"Unicode hostname with underscore", "bücher_server.example:8443", true, "https://xn--bcher_server-dlb.example:8443"},
		{"Unicode beside ASCII consecutive hyphens", "ab--cd.bücher.example:8443", true, "https://ab--cd.xn--bcher-kva.example:8443"},
		{"whitespace", " \t https://bootstrap:8080/path \n", true, "https://bootstrap:8080"},
		{"invalid URL", "https://", true, ""},
		{"invalid hostname", "https://bootstrap..example:443", true, ""},
		{"empty present option", "", true, ""},
		{"absent option", "", false, ""},
	}
	for _, code := range []int{143, 136} {
		for _, tt := range tests {
			t.Run(fmtOptionName(code, tt.name), func(t *testing.T) {
				logger := logrus.New()
				logger.SetOutput(io.Discard)
				hook := &optionLogHook{}
				logger.AddHook(hook)
				if got := serverFromOption(logger, "eth-test", code, []byte(tt.data), tt.present); got != tt.want {
					t.Fatalf("serverFromOption() = %q, want %q", got, tt.want)
				}
				if len(hook.entries) != 1 {
					t.Fatalf("got %d log entries, want 1", len(hook.entries))
				}
				entry := hook.entries[0]
				if entry.Data["interface"] != "eth-test" || entry.Data["option"] != code {
					t.Errorf("missing interface/option fields: %v", entry.Data)
				}
				missing := strings.Contains(entry.Message, "not found")
				if missing == tt.present {
					t.Errorf("missing warning = %v for present = %v: %s", missing, tt.present, entry.Message)
				}
				if tt.want != "" {
					if entry.Level != logrus.InfoLevel || !strings.Contains(entry.Message, tt.want) {
						t.Errorf("success log must contain canonical origin: %v", entry)
					}
				} else if entry.Level != logrus.WarnLevel {
					t.Errorf("invalid or missing option log level = %v, want warn", entry.Level)
				}
				if tt.present && tt.want == "" && entry.Data["error"] == nil {
					t.Errorf("invalid present option log lacks normalization error: %v", entry.Data)
				}
			})
		}
	}
}

func fmtOptionName(option int, name string) string {
	if option == 143 {
		return "DHCP4/" + name
	}
	return "DHCP6/" + name
}
