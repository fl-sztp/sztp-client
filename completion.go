// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"fl-sztp-client/sztpclient"
)

type reportFunc func(context.Context, sztpclient.CompletionReport) error

func completionReporter(logger *slog.Logger, config *Config) reportFunc {
	return func(ctx context.Context, report sztpclient.CompletionReport) error {
		client := sztpclient.NewClient(logger, config.AllowInsecure, "", "", report.SerialNumber,
			config.DevicePassword, config.DevicePrivateKey, config.DeviceEndEntityCert, config.BootstrapTrustAnchor)
		return client.ReportCompletion(ctx, report)
	}
}

func retryCompletionReport(ctx context.Context, logger *slog.Logger, report sztpclient.CompletionReport,
	send reportFunc, wait func(context.Context, time.Duration) error,
) error {
	delays := [...]time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 30 * time.Minute}
	step := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := send(ctx, report)
		if err == nil {
			return nil
		}
		if errors.Is(err, sztpclient.ErrReportCredentials) {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		delay := delays[step]
		logger.Warn("Completion report failed; retrying", "error", err, "delay", delay)
		if err := wait(ctx, delay); err != nil {
			return err
		}
		if step < len(delays)-1 {
			step++
		}
	}
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
