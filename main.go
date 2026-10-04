/*
SPDX-License-Identifier: Apache-2.0
Copyright (C) 2025 Anssi Söderena, Sandip Ghimire
Modifications Copyright (C) 2026 Anssi Söderena

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fl-sztp-client/dhcpclient"
	"fl-sztp-client/serveraddr"
	"fl-sztp-client/sztpclient"
	"os"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	FALLBACK_SERVER = "xverse.fusionlayer.com:9090"
)

func main() {

	var logger = logrus.New()
	// Set the log output to stdout with timestamp
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	config, err := getConfig()
	if err != nil {
		logger.Fatalf("Failed to initialize configuration: %v", err)
	}

	// If the --debug flag is provided, enable debug mode
	if config.UseDebug {
		logger.SetLevel(logrus.DebugLevel)
		// logger.SetReportCaller(true)
		logger.Debug("Debug mode enabled")
	} else {
		logger.SetLevel(logrus.InfoLevel)
	}

	// try to create tmp folder if it doesn't exist
	if _, err := os.Stat(config.TmpFolder); os.IsNotExist(err) {
		err = os.Mkdir(config.TmpFolder, 0755)
		if err != nil {
			logger.Fatalf("tmp folder %s doesn't exist and failed to create it: %v", config.TmpFolder, err)
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
			server = discoverServerWithRetry(logger, dhcpclient.GetServerFromDHCP6, dhcpclient.GetServerFromDHCP4, 10, time.Minute, time.Sleep)
			if server == "" {
				logger.Error("Failed to discover server address via DHCP after 10 attempts")
				server, err = serveraddr.Normalize(FALLBACK_SERVER)
				if err != nil {
					logger.Fatalf("Failed to normalize fallback server address: %v", err)
				}
				logger.Info("Using fallback server address: ", server)
			}
		}

		// Output the server address
		logger.Info("Using server address: ", server)

		// Create a new bootstrap client instance
		client := sztpclient.NewClient(logger, config.AllowInsecure, server, config.TmpFolder, config.DeviceSerialNumber, config.DevicePassword, config.DevicePrivateKey, config.DeviceEndEntityCert, config.BootstrapTrustAnchor)

		err = client.RunBootstrapSequence()
		if err != nil {
			logger.Error("Bootstrap sequence failed: ", err)
			bootstrapRetryNum += 1
			logger.Warnf("Retrying the bootstrap process in 1 minute... (retry %d/∞)", bootstrapRetryNum)
			time.Sleep(1 * time.Minute)
		} else {
			bootstrapSuccessful = true
		}

	}
}
