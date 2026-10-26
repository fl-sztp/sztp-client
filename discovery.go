// SPDX-FileCopyrightText: 2025 Anssi Söderena <https://github.com/asode>
// SPDX-FileCopyrightText: 2025 Sandip Ghimire <https://github.com/sandip-ghimire>
// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode> (modifications)
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"log/slog"
	"time"
)

type discoveryFunc func(*slog.Logger) (string, error)

func discoverServerOnce(logger *slog.Logger, v6, v4 discoveryFunc) string {
	logger.Info("Discovering server address", "protocol", "DHCPv6")
	server, err := v6(logger)
	if err != nil {
		logger.Warn("Failed to discover server address", "protocol", "DHCPv6", "error", err)
	} else if server != "" {
		return server
	}

	logger.Info("Discovering server address", "protocol", "DHCPv4")
	server, err = v4(logger)
	if err != nil {
		logger.Warn("Failed to discover server address", "protocol", "DHCPv4", "error", err)
		return ""
	}
	return server
}

func discoverServerWithRetry(logger *slog.Logger, v6, v4 discoveryFunc, attempts int, delay time.Duration, sleep func(time.Duration)) string {
	for attempt := 1; attempt <= attempts; attempt++ {
		if server := discoverServerOnce(logger, v6, v4); server != "" {
			return server
		}
		logger.Warn("DHCP discovery attempt failed", "attempt", attempt, "max_attempts", attempts)
		if attempt < attempts {
			logger.Warn("Retrying DHCP discovery", "event", "dhcp_retry_scheduled", "attempt", attempt, "delay", delay)
			sleep(delay)
		}
	}
	return ""
}
