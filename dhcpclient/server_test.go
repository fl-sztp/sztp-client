// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package dhcpclient

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

type optionLog struct {
	Level     string `json:"level"`
	Message   string `json:"msg"`
	Interface string `json:"interface"`
	Option    int    `json:"option"`
	Error     string `json:"error"`
	Server    string `json:"server"`
}

func newOptionLogger() (*slog.Logger, *bytes.Buffer) {
	output := new(bytes.Buffer)
	return slog.New(slog.NewJSONHandler(output, nil)), output
}

func readOptionLog(t *testing.T, output *bytes.Buffer) optionLog {
	t.Helper()
	var entry optionLog
	// Unmarshal rejects missing records and more than one JSON record.
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatalf("expected one JSON log record: %v; output=%s", err, output)
	}
	return entry
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
				logger, output := newOptionLogger()
				if got := serverFromOption(logger, "eth-test", code, []byte(tt.data), tt.present); got != tt.want {
					t.Fatalf("serverFromOption() = %q, want %q", got, tt.want)
				}
				entry := readOptionLog(t, output)
				if entry.Interface != "eth-test" || entry.Option != code {
					t.Errorf("missing interface/option fields: %v", entry)
				}
				missing := strings.Contains(entry.Message, "not found")
				if missing == tt.present {
					t.Errorf("missing warning = %v for present = %v: %s", missing, tt.present, entry.Message)
				}
				if tt.want != "" {
					if entry.Level != "INFO" || entry.Server != tt.want {
						t.Errorf("success log must contain canonical origin: %v", entry)
					}
				} else if entry.Level != "WARN" {
					t.Errorf("invalid or missing option log level = %v, want warn", entry.Level)
				}
				if tt.present && tt.want == "" && entry.Error == "" {
					t.Errorf("invalid present option log lacks normalization error: %v", entry)
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
