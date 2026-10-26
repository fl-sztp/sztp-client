// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bytes"
	"log/slog"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"fl-sztp-client/internal/logging"
)

// logRecords reads complete TextHandler lines. Decode fields in order so text
// inside a quoted message cannot impersonate an event or server field.
func logRecords(output []byte) []map[string]string {
	var records []map[string]string
	lines := bytes.Split(output, []byte{'\n'})
	for _, line := range lines[:len(lines)-1] {
		if record, ok := parseLogRecord(string(line)); ok {
			records = append(records, record)
		}
	}
	return records
}

func parseLogRecord(line string) (map[string]string, bool) {
	record := make(map[string]string)
	for line != "" {
		key, rest, ok := strings.Cut(line, "=")
		if !ok || key == "" || strings.ContainsAny(key, " \t\r\n\"") {
			return nil, false
		}
		if _, duplicate := record[key]; duplicate {
			return nil, false
		}
		var value string
		if strings.HasPrefix(rest, `"`) {
			quoted, err := strconv.QuotedPrefix(rest)
			if err != nil {
				return nil, false
			}
			value, err = strconv.Unquote(quoted)
			if err != nil {
				return nil, false
			}
			rest = rest[len(quoted):]
			if rest != "" && rest[0] != ' ' {
				return nil, false
			}
			line = strings.TrimPrefix(rest, " ")
		} else {
			value, line, _ = strings.Cut(rest, " ")
			if value == "" || strings.ContainsAny(value, "\"\t\r\n=") {
				return nil, false
			}
		}
		record[key] = value
	}
	return record, len(record) != 0
}

func TestLogRecords(t *testing.T) {
	for _, server := range []string{
		"https://bootstrap:8080", "https://[2001:db8::1]:8443",
		"https://user:p=ss@bücher.example:8443", "quotes\" and \\slashes\nnext line",
	} {
		t.Run(server, func(t *testing.T) {
			var output bytes.Buffer
			logger := logging.New(&output, true)
			logger.Info("Server selected", "event", "bootstrap_server_selected", "server", server)
			logger.Warn("Retry", "event", "dhcp_retry_scheduled", "attempt", 1, "delay", "1m0s")
			records := logRecords(output.Bytes())
			if len(records) != 2 || records[0]["server"] != server || records[0]["event"] != "bootstrap_server_selected" ||
				records[1]["event"] != "dhcp_retry_scheduled" || records[1]["attempt"] != "1" || records[1]["delay"] != "1m0s" {
				t.Fatalf("incorrect log records: %#v", records)
			}
			if got := logRecords(output.Bytes()[:output.Len()-1]); len(got) != 1 {
				t.Fatalf("accepted unfinished record: %#v", got)
			}
		})
	}
}

func TestLogRecordsRejectMalformedAndEmbeddedEvents(t *testing.T) {
	for _, line := range []string{
		`msg="unterminated`, `msg="bad\q"`, `msg="ok"suffix`, `event=`,
		`event=one event=two`, `not a field`, `event=one garbage`,
	} {
		if got := logRecords([]byte(line + "\n")); len(got) != 0 {
			t.Errorf("accepted malformed line %q: %#v", line, got)
		}
	}
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	message := `event=bootstrap_server_selected server=https://fake.example`
	logger.Info(message, "response", message)
	records := logRecords(output.Bytes())
	if len(records) != 1 || records[0]["event"] != "" || records[0]["server"] != "" || records[0]["msg"] != message {
		t.Fatalf("message impersonated fields: %#v", records)
	}
	got, ok := parseLogRecord(`msg="" event=dhcp_retry_scheduled`)
	if !ok || !reflect.DeepEqual(got, map[string]string{"msg": "", "event": "dhcp_retry_scheduled"}) {
		t.Fatalf("empty message: %#v, %t", got, ok)
	}
}
