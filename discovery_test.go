// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"fl-sztp-client/internal/logging"
)

func discoveryTestLogger() *slog.Logger {
	logger := logging.New(io.Discard, false)
	return logger
}

func TestDiscoverServerOnce(t *testing.T) {
	failure := errors.New("discovery failed")
	for _, tc := range []struct {
		name, v4Server, v6Server, want string
		v4Err, v6Err                   error
		wantV6                         bool
	}{
		{name: "v4 success skips v6", v4Server: "https://v4.example", v6Server: "https://v6.example", want: "https://v4.example"},
		{name: "v4 empty uses v6", v6Server: "https://v6.example", want: "https://v6.example", wantV6: true},
		{name: "v4 error uses v6", v4Err: failure, v6Server: "https://v6.example", want: "https://v6.example", wantV6: true},
		{name: "v6 error", v6Err: failure, wantV6: true},
		{name: "both empty", wantV6: true},
		{name: "both errors", v4Err: failure, v6Err: failure, wantV6: true},
		{name: "v4 address with error rejected", v4Server: "https://bad.example", v4Err: failure, v6Server: "https://v6.example", want: "https://v6.example", wantV6: true},
		{name: "v6 address with error rejected", v6Server: "https://bad.example", v6Err: failure, wantV6: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v4Calls, v6Calls := 0, 0
			got := discoverServerOnce(discoveryTestLogger(), func(*slog.Logger) (string, error) {
				v4Calls++
				return tc.v4Server, tc.v4Err
			}, func(*slog.Logger) (string, error) {
				v6Calls++
				return tc.v6Server, tc.v6Err
			})
			if got != tc.want {
				t.Fatalf("server = %q, want %q", got, tc.want)
			}
			wantV6Calls := 0
			if tc.wantV6 {
				wantV6Calls = 1
			}
			if v4Calls != 1 || v6Calls != wantV6Calls {
				t.Fatalf("calls v4=%d v6=%d, want 1/%d", v4Calls, v6Calls, wantV6Calls)
			}
		})
	}
}

func TestDiscoverServerWithRetry(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		attempts, successRound int
		wantEvents             []string
	}{
		{name: "second round succeeds", attempts: 3, successRound: 2, wantEvents: []string{"v4", "v6", "wait", "v4", "v6"}},
		{name: "first round succeeds", attempts: 10, successRound: 1, wantEvents: []string{"v4", "v6"}},
		{name: "final failure has no wait", attempts: 2, wantEvents: []string{"v4", "v6", "wait", "v4", "v6"}},
		{name: "no attempts", attempts: 0, wantEvents: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := []string{}
			round := 0
			got := discoverServerWithRetry(discoveryTestLogger(), func(*slog.Logger) (string, error) {
				round++
				events = append(events, "v4")
				return "", nil
			}, func(*slog.Logger) (string, error) {
				events = append(events, "v6")
				if round == tc.successRound {
					return "https://server.example", nil
				}
				return "", nil
			}, tc.attempts, time.Minute, func(delay time.Duration) {
				if delay != time.Minute {
					t.Errorf("delay = %v, want 1m", delay)
				}
				events = append(events, "wait")
			})
			want := ""
			if tc.successRound > 0 {
				want = "https://server.example"
			}
			if got != want {
				t.Errorf("server = %q, want %q", got, want)
			}
			if !reflect.DeepEqual(events, tc.wantEvents) {
				t.Errorf("events = %v, want %v", events, tc.wantEvents)
			}
		})
	}
}

func TestDiscoverServerWithRetryTenFailedRounds(t *testing.T) {
	v4Calls, v6Calls, sleeps := 0, 0, 0
	got := discoverServerWithRetry(discoveryTestLogger(), func(*slog.Logger) (string, error) {
		v4Calls++
		return "", nil
	}, func(*slog.Logger) (string, error) {
		v6Calls++
		return "", nil
	}, 10, time.Minute, func(delay time.Duration) {
		if delay != time.Minute {
			t.Errorf("delay = %v, want 1m", delay)
		}
		sleeps++
	})
	if got != "" || v4Calls != 10 || v6Calls != 10 || sleeps != 9 {
		t.Fatalf("server=%q v4=%d v6=%d sleeps=%d, want empty/10/10/9", got, v4Calls, v6Calls, sleeps)
	}
}
