// SPDX-FileCopyrightText: 2025 Anssi Söderena <https://github.com/asode>
// SPDX-FileCopyrightText: 2025 Sandip Ghimire <https://github.com/sandip-ghimire>
// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode> (modifications)
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fl-sztp-client/dhcpclient"
	"fl-sztp-client/internal/logging"
	"fl-sztp-client/serveraddr"
	"fl-sztp-client/sztpclient"
)

const (
	FALLBACK_SERVER = "xverse.fusionlayer.com:9090"
)

func main() {
	runClient(getConfig)
}

func runClient(loadConfig func() (*Config, error)) {
	logger := logging.New(os.Stderr, false)
	slog.SetDefault(logger)

	config, err := loadConfig()
	if err != nil {
		logger.Error("Failed to initialize configuration", "error", err)
		os.Exit(1)
	}

	logger = logging.New(os.Stderr, config.UseDebug)
	slog.SetDefault(logger)
	logger.Debug("Debug mode enabled")
	if config.configPath != "" {
		logger.Info("Using configuration file", "path", config.configPath)
	} else {
		logger.Info("Using configuration defaults and command-line settings")
	}
	if config.DiscoverOnly {
		os.Exit(runDiscoveryReport(os.Stdout, logger, config, sztpclient.DiscoverDeviceInformation, dhcpclient.GetServerFromDHCP4, dhcpclient.GetServerFromDHCP6))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runOnboarding(ctx, logger, config, func(ctx context.Context) (sztpclient.CompletionReport, error) {
		return runBootstrap(ctx, logger, config)
	}, completionReporter(logger, config)); err != nil {
		logger.Error("Onboarding failed", "error", err)
		os.Exit(1)
	}
}

// Keep the state lifecycle outside the bootstrap retry loop. In particular, a
// failed completion write must never retry scripts that just succeeded.
func runOnboarding(ctx context.Context, logger *slog.Logger, config *Config,
	bootstrap func(context.Context) (sztpclient.CompletionReport, error), report reportFunc,
) (err error) {
	logger.Info("Using state directory", "path", config.StateDir)
	store, err := openOnboardingStore(config.StateDir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, store.Close()) }()
	if err := store.CleanupWorkspace(); err != nil {
		return err
	}
	if config.ResetOnboarding {
		if err := store.Reset(); err != nil {
			return err
		}
		logger.Info("Onboarding state reset; the next normal start will bootstrap the device")
		return nil
	}
	state, err := store.Read()
	if err != nil {
		return err
	}
	if state != nil && state.ReportStatus != reportPending {
		logger.Info("Device already onboarded; skipping bootstrap", "completed_at", state.CompletedAt)
		return nil
	}
	if state == nil {
		if err := validateBootstrapConfig(config); err != nil {
			return err
		}
	} else if err := validateReportingConfig(config); err != nil {
		return err
	}
	if err := store.CheckWritable(); err != nil {
		return err
	}
	if state == nil {
		parent := config.TmpFolder
		workspace, err := prepareTmpFolder(logger, parent)
		if err != nil {
			return err
		}
		config.TmpFolder = workspace
		defer func() { config.TmpFolder = parent }()
		if err := store.RegisterWorkspace(config.TmpFolder); err != nil {
			return errors.Join(err, os.RemoveAll(config.TmpFolder))
		}
		defer func() { err = errors.Join(err, store.CleanupWorkspace()) }()
		logger.Info("Using temporary folder", "path", config.TmpFolder)
		result, err := bootstrap(ctx)
		if err != nil {
			return err
		}
		state = &onboardingState{Version: 2, CompletedAt: time.Now().UTC(), SerialNumber: result.SerialNumber,
			Report: &result, ReportStatus: reportPending}
		if err := store.Save(*state); err != nil {
			return err
		}
		logger.Info("Local onboarding complete; completion report pending")
		if err := store.CleanupWorkspace(); err != nil {
			return err
		}
	}
	if err := retryCompletionReport(ctx, logger, *state.Report, report, waitForRetry); err != nil {
		return err
	}
	state.ReportStatus = reportAcknowledged
	if err := store.Save(*state); err != nil {
		return err
	}
	logger.Info("Completion report acknowledged; completion state saved")
	return nil
}

func runBootstrap(ctx context.Context, logger *slog.Logger, config *Config) (sztpclient.CompletionReport, error) {
	return runBootstrapWithRetry(ctx, logger, config, waitForRetry)
}

func runBootstrapWithRetry(ctx context.Context, logger *slog.Logger, config *Config,
	wait func(context.Context, time.Duration) error,
) (sztpclient.CompletionReport, error) {

	// If the server address is provided via command line or config file, use it
	server := config.Server

	// Perform the bootstrap sequence until the device is successfully onboarded
	bootstrapRetryNum := 0

	// TODO: stop at some point?
	for {
		if err := ctx.Err(); err != nil {
			return sztpclient.CompletionReport{}, err
		}

		// If the server address is not provided, try to discover it via DHCP
		if server == "" {
			server = discoverServerWithRetry(logger, dhcpclient.GetServerFromDHCP4, dhcpclient.GetServerFromDHCP6, 10, time.Minute, time.Sleep)
			if server == "" {
				logger.Warn("DHCP discovery exhausted", "attempt", 10)
				fallback, err := serveraddr.Normalize(FALLBACK_SERVER)
				if err != nil {
					return sztpclient.CompletionReport{}, err
				}
				server = fallback
				logger.Info("Using fallback server address", "server", server)
			}
		}

		// Output the server address
		logger.Info("Using server address", "event", "bootstrap_server_selected", "server", server)

		// Create a new bootstrap client instance
		client := sztpclient.NewClient(logger, config.AllowInsecure, server, config.TmpFolder, config.DeviceSerialNumber, config.DevicePassword, config.DevicePrivateKey, config.DeviceEndEntityCert, config.BootstrapTrustAnchor)
		client.RequestLimits = config.RequestLimits

		result, err := client.RunBootstrapSequence(ctx)
		if err != nil {
			if cleanupErr := clearWorkspace(config.TmpFolder); cleanupErr != nil {
				return sztpclient.CompletionReport{}, errors.Join(err, cleanupErr)
			}
			if ctx.Err() != nil {
				return sztpclient.CompletionReport{}, ctx.Err()
			}
			bootstrapRetryNum += 1
			logger.Warn("Bootstrap failed; retrying", "error", err, "attempt", bootstrapRetryNum, "delay", time.Minute)
			if err := wait(ctx, time.Minute); err != nil {
				return sztpclient.CompletionReport{}, err
			}
		} else {
			return result, nil
		}

	}
}
