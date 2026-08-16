//go:build linux
// +build linux

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

package dhcpclient

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
	"github.com/sirupsen/logrus"
)

func GetServerFromDHCP4(logger *logrus.Logger) (string, error) {

	// TODO: support for multiple servers?

	interfaces, err := net.Interfaces()
	if err != nil {
		logger.Warn("Failed to get network interfaces for discovering the Bootstrap server via DHCP4: ", err)
	}
	for _, iface := range interfaces {
		// Skip loopback interfaces
		if iface.Flags&net.FlagLoopback != 0 {
			logger.Debug("Skipping loopback device as it cannot be used for discovering the Bootstrap server via DHCP4", iface.Name)
			continue
		}
		if iface.Flags&net.FlagRunning == 0 {
			logger.Debug("Skipping device as it is not running and cannot be used for discovering the Bootstrap server via DHCP4", iface.Name)
			continue
		}
		logger.Debug("Trying with device for discovering the Bootstrap server via DHCP4", iface.Name)
		nc, err := nclient4.New(iface.Name)
		if err != nil {
			return "", fmt.Errorf("failed to create DHCPv4 client for discovering the Bootstrap server via DHCP4: %w", err)
		}
		offer, err := nc.DiscoverOffer(context.Background(), dhcpv4.WithRequestedOptions(dhcpv4.OptionOPTIONIPv6AddressANDSF))
		if err != nil {
			logger.Warn("Failed to discover the Bootstrap server via DHCP4: ", err)
			nc.Close()
			continue
		}
		logger.Debug("DHCP4 offer received: ", offer.Summary())

		// Parse option 143 from the DHCP offer
		optionDataBytes := offer.GetOneOption(dhcpv4.GenericOptionCode(143))
		if optionDataBytes != nil {
			// cast optionData to string and trim spaces
			optionDataString := strings.TrimSpace(string(optionDataBytes))
			logger.Debug("Option 143 found in DHCP4 offer: ", optionDataString)
			if isValidAddress(optionDataString) {
				logger.Info("Bootstrap server address discovered via DHCP4: ", optionDataString)
				nc.Close()
				return optionDataString, nil
			} else {
				logger.Warn("Invalid address discovered via DHCP4: ", optionDataString)
			}
		}

		logger.Warn("Option 143 not found in DHCP4 offer")
		nc.Close()
	}

	logger.Warn("No valid Bootstrap server address discovered via DHCP4")
	return "", nil
}

func isValidAddress(addr string) bool {
	_, err := net.ResolveTCPAddr("tcp", addr)
	return err == nil
}
