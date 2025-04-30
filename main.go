/*
SPDX-License-Identifier: Apache-2.0
Copyright (C) 2025 Anssi Söderena, Sandip Ghimire

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
	"fl-sztp-client/sztpclient"
	"os"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	FALLBACK_SERVER = "xverse.fusionlayer.com:9090"
)

func getServerAddressFromDHCP(logger *logrus.Logger) string {
	server := ""

	var dhcpError error
	logger.Info("No server address provided, discovering via DHCP6...")
	server, dhcpError = dhcpclient.GetServerFromDHCP6(logger)
	if dhcpError != nil {
		logger.Warn("Failed to discover server address via DHCP6: ", dhcpError)
	}

	if server == "" {
		var dhcpError error
		logger.Info("No server address provided, discovering via DHCP4...")
		server, dhcpError = dhcpclient.GetServerFromDHCP4(logger)
		if dhcpError != nil {
			logger.Warn("Failed to discover server address via DHCP: ", dhcpError)
		}
	}

	return server
}

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
			DHCPRetries := 10
			for i := 0; i < DHCPRetries && server == ""; i++ {
				server = getServerAddressFromDHCP(logger)
				if server == "" {
					logger.Error("Failed to discover server address via DHCP")
					logger.Warnf("Retrying the DHCP discovery in 1 minute... Retry %d/%d", i+1, DHCPRetries)
					time.Sleep(1 * time.Minute)
				}
			}
			if server == "" {
				logger.Error("Failed to discover server address via DHCP after 10 retries")
				logger.Info("Using fallback server address: ", FALLBACK_SERVER)
				server = FALLBACK_SERVER
			}
		}

		// Use (force) HTTPS for the request
		if strings.HasPrefix(server, "http://") {
			server = "https://" + strings.TrimPrefix(server, "http://")
		} else if !strings.HasPrefix(server, "https://") {
			server = "https://" + server
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
