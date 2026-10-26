// SPDX-FileCopyrightText: 2025 Anssi Söderena <https://github.com/asode>
// SPDX-FileCopyrightText: 2025 Sandip Ghimire <https://github.com/sandip-ghimire>
// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode> (modifications)
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fl-sztp-client/dhcpclient"
	"fl-sztp-client/serveraddr"
	"fl-sztp-client/sztpclient"
	"os"
	"time"

	"fl-sztp-client/internal/logging"
	"log/slog"
)

const (
	FALLBACK_SERVER = "xverse.fusionlayer.com:9090"
)

func main() {
	logger := logging.New(os.Stderr, false)
	slog.SetDefault(logger)

	config, err := getConfig()
	if err != nil {
		logger.Error("Failed to initialize configuration", "error", err)
		os.Exit(1)
	}

	logger = logging.New(os.Stderr, config.UseDebug)
	slog.SetDefault(logger)
	logger.Debug("Debug mode enabled")

	if config.DiscoverOnly {
		os.Exit(runDiscoveryReport(os.Stdout, logger, config, sztpclient.DiscoverDeviceInformation, dhcpclient.GetServerFromDHCP4, dhcpclient.GetServerFromDHCP6))
	}

	// try to create tmp folder if it doesn't exist
	if _, err := os.Stat(config.TmpFolder); os.IsNotExist(err) {
		err = os.Mkdir(config.TmpFolder, 0755)
		if err != nil {
			logger.Error("Failed to create temporary folder", "path", config.TmpFolder, "error", err)
			os.Exit(1)
		}
	}

	// If the server address is provided via command line or config file, use it
	server := config.Server

	// Perform the bootstrap sequence until the device is successfully onboarded
	bootstrapSuccessful := false
	bootstrapRetryNum := 0

	// TODO: stop at some point?
	for !bootstrapSuccessful {

		// If the server address is not provided, try to discover it via DHCP
		if server == "" {
			server = discoverServerWithRetry(logger, dhcpclient.GetServerFromDHCP4, dhcpclient.GetServerFromDHCP6, 10, time.Minute, time.Sleep)
			if server == "" {
				logger.Warn("DHCP discovery exhausted", "attempt", 10)
				server, err = serveraddr.Normalize(FALLBACK_SERVER)
				if err != nil {
					logger.Error("Failed to normalize fallback server address", "error", err)
					os.Exit(1)
				}
				logger.Info("Using fallback server address", "server", server)
			}
		}

		// Output the server address
		logger.Info("Using server address", "event", "bootstrap_server_selected", "server", server)

		// Create a new bootstrap client instance
		client := sztpclient.NewClient(logger, config.AllowInsecure, server, config.TmpFolder, config.DeviceSerialNumber, config.DevicePassword, config.DevicePrivateKey, config.DeviceEndEntityCert, config.BootstrapTrustAnchor)

		err = client.RunBootstrapSequence()
		if err != nil {
			logger.Warn("Bootstrap failed; retrying", "error", err, "attempt", bootstrapRetryNum+1, "delay", time.Minute)
			bootstrapRetryNum += 1
			time.Sleep(1 * time.Minute)
		} else {
			bootstrapSuccessful = true
		}

	}
}
