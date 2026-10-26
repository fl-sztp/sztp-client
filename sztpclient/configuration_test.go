//go:build linux || darwin

// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package sztpclient

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestErrorsAreReturnedWithoutDuplicateLogs(t *testing.T) {
	for _, kind := range []string{"transport", "body", "decode"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch kind {
				case "transport":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = conn.Close()
				case "body":
					w.Header().Set("Content-Length", "100")
					_, _ = w.Write([]byte("short"))
				case "decode":
					_, _ = w.Write([]byte("not JSON"))
				}
			}))
			defer server.Close()
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
			client := NewClient(logger, false, server.URL, "", "test-device", "", "", "", "")
			_, err := client.postRequest("{}", server.URL, false, http.StatusOK)
			if err == nil {
				t.Fatal("request unexpectedly succeeded")
			}
			if kind == "body" && !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("lost read failure: %v", err)
			}
			if kind == "decode" && !strings.Contains(err.Error(), "not JSON") {
				t.Fatalf("lost response context: %v", err)
			}
			if logs.Len() != 0 {
				t.Fatalf("returned error was also logged: %s", &logs)
			}
		})
	}
}
