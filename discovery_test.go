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
		name, v6Server, v4Server, want string
		v6Err, v4Err                   error
		wantV4                         bool
	}{
		{name: "v6 success skips v4", v6Server: "https://v6.example", v4Server: "https://v4.example", want: "https://v6.example"},
		{name: "v6 empty uses v4", v4Server: "https://v4.example", want: "https://v4.example", wantV4: true},
		{name: "v6 error uses v4", v6Err: failure, v4Server: "https://v4.example", want: "https://v4.example", wantV4: true},
		{name: "v4 error", v4Err: failure, wantV4: true},
		{name: "both empty", wantV4: true},
		{name: "both errors", v6Err: failure, v4Err: failure, wantV4: true},
		{name: "v6 address with error rejected", v6Server: "https://bad.example", v6Err: failure, v4Server: "https://v4.example", want: "https://v4.example", wantV4: true},
		{name: "v4 address with error rejected", v4Server: "https://bad.example", v4Err: failure, wantV4: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v6Calls, v4Calls := 0, 0
			got := discoverServerOnce(discoveryTestLogger(), func(*slog.Logger) (string, error) {
				v6Calls++
				return tc.v6Server, tc.v6Err
			}, func(*slog.Logger) (string, error) {
				v4Calls++
				return tc.v4Server, tc.v4Err
			})
			if got != tc.want {
				t.Fatalf("server = %q, want %q", got, tc.want)
			}
			wantV4Calls := 0
			if tc.wantV4 {
				wantV4Calls = 1
			}
			if v6Calls != 1 || v4Calls != wantV4Calls {
				t.Fatalf("calls v6=%d v4=%d, want 1/%d", v6Calls, v4Calls, wantV4Calls)
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
		{name: "second round succeeds", attempts: 3, successRound: 2, wantEvents: []string{"v6", "v4", "wait", "v6", "v4"}},
		{name: "first round succeeds", attempts: 10, successRound: 1, wantEvents: []string{"v6", "v4"}},
		{name: "final failure has no wait", attempts: 2, wantEvents: []string{"v6", "v4", "wait", "v6", "v4"}},
		{name: "no attempts", attempts: 0, wantEvents: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := []string{}
			round := 0
			got := discoverServerWithRetry(discoveryTestLogger(), func(*slog.Logger) (string, error) {
				round++
				events = append(events, "v6")
				return "", nil
			}, func(*slog.Logger) (string, error) {
				events = append(events, "v4")
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
	v6Calls, v4Calls, sleeps := 0, 0, 0
	got := discoverServerWithRetry(discoveryTestLogger(), func(*slog.Logger) (string, error) {
		v6Calls++
		return "", nil
	}, func(*slog.Logger) (string, error) {
		v4Calls++
		return "", nil
	}, 10, time.Minute, func(delay time.Duration) {
		if delay != time.Minute {
			t.Errorf("delay = %v, want 1m", delay)
		}
		sleeps++
	})
	if got != "" || v6Calls != 10 || v4Calls != 10 || sleeps != 9 {
		t.Fatalf("server=%q v6=%d v4=%d sleeps=%d, want empty/10/10/9", got, v6Calls, v4Calls, sleeps)
	}
}
