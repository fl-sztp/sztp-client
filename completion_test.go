// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"fl-sztp-client/sztpclient"
)

func TestCompletionReportBackoffAndSuccess(t *testing.T) {
	for _, failure := range []error{errors.New("HTTP 503"), context.DeadlineExceeded} {
		t.Run(failure.Error(), func(t *testing.T) {
			var delays []time.Duration
			calls := 0
			report := sztpclient.CompletionReport{Endpoint: "https://saved.example/report", SerialNumber: "saved-serial"}
			send := func(_ context.Context, got sztpclient.CompletionReport) error {
				if got != report || len(delays) != calls {
					t.Fatal("report changed or an attempt did not immediately follow its wait")
				}
				calls++
				if calls == 9 {
					return nil
				}
				return failure
			}
			wait := func(_ context.Context, delay time.Duration) error {
				delays = append(delays, delay)
				return nil
			}
			err := retryCompletionReport(context.Background(), tmpTestLogger(new(bytes.Buffer)), report, send, wait)
			want := []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 30 * time.Minute, 30 * time.Minute, 30 * time.Minute}
			if err != nil || calls != 9 || !reflect.DeepEqual(delays, want) {
				t.Fatalf("calls=%d delays=%v error=%v", calls, delays, err)
			}
		})
	}
}

func TestCompletionReportCancellation(t *testing.T) {
	for _, duringWait := range []bool{false, true} {
		t.Run(map[bool]string{false: "request", true: "wait"}[duringWait], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls, waits := 0, 0
			send := func(ctx context.Context, _ sztpclient.CompletionReport) error {
				calls++
				if !duringWait {
					cancel()
					<-ctx.Done()
					return ctx.Err()
				}
				return errors.New("offline")
			}
			wait := func(ctx context.Context, _ time.Duration) error {
				waits++
				cancel()
				return waitForRetry(ctx, 30*time.Minute)
			}
			err := retryCompletionReport(ctx, tmpTestLogger(new(bytes.Buffer)), sztpclient.CompletionReport{}, send, wait)
			if !errors.Is(err, context.Canceled) || calls != 1 || (duringWait && waits != 1) || (!duringWait && waits != 0) {
				t.Fatalf("calls=%d waits=%d error=%v", calls, waits, err)
			}
		})
	}
}

func TestCompletionReportMissingCredentialsStopsRetries(t *testing.T) {
	calls := 0
	err := retryCompletionReport(context.Background(), tmpTestLogger(new(bytes.Buffer)), sztpclient.CompletionReport{},
		func(context.Context, sztpclient.CompletionReport) error {
			calls++
			return sztpclient.ErrReportCredentials
		},
		func(context.Context, time.Duration) error {
			t.Fatal("credential failure scheduled a retry")
			return nil
		})
	if !errors.Is(err, sztpclient.ErrReportCredentials) || calls != 1 {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
}
