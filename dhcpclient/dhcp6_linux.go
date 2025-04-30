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
	"net"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/dhcpv6/nclient6"
	"github.com/sirupsen/logrus"
)

func GetServerFromDHCP6(logger *logrus.Logger) (string, error) {

	// TODO: support for multiple servers?

	interfaces, err := net.Interfaces()
	if err != nil {
		logger.Warn("Failed to get network interfaces for discovering the Bootstrap server via DHCP6: ", err)
	}
	for _, iface := range interfaces {
		// Skip loopback interfaces
		if iface.Flags&net.FlagLoopback != 0 {
			logger.Debug("Skipping loopback device as it cannot be used for discovering the Bootstrap server via DHCP6: ", iface.Name)
			continue
		}
		if iface.Flags&net.FlagRunning == 0 {
			logger.Debug("Skipping device as it is not running and cannot be used for discovering the Bootstrap server via DHCP6: ", iface.Name)
			continue
		}
		logger.Debug("Trying with device for discovering the Bootstrap server via DHCP6: ", iface.Name)
		nc, err := nclient6.New(iface.Name)
		if err != nil {
			logger.Warn("Failed to create DHCPv6 client for discovering the Bootstrap server via DHCP6: ", err)
			continue
		}
		solicit, err := nc.Solicit(context.Background(), dhcpv6.WithRequestedOptions(dhcpv6.OptionCode(136)))
		if err != nil {
			logger.Warn("Failed to solicit the Bootstrap server via DHCP6: ", err)
			nc.Close()
			continue
		}
		logger.Debug("DHCP6 solicit received SUMAMRY: ", solicit.Summary())

		// Parse option 136 from the DHCP solicit
		optionData := solicit.GetOneOption(dhcpv6.OptionCode(136))
		if optionData != nil {
			logger.Debug("Option 136 found in DHCP6 solicit: ", optionData)
			optionDataString := string(optionData.ToBytes())
			logger.Debug("Option 136 found in DHCP6 solicit STRING: ", optionDataString)
			if isValidAddress(optionDataString) {
				logger.Info("Bootstrap server address discovered via DHCP6: ", optionDataString)
				nc.Close()
				return optionDataString, nil
			} else {
				logger.Warn("Invalid address discovered via DHCP6: ", optionDataString)
			}
		} else {
			logger.Warn("Option 136 not found in DHCP6 solicit")
		}
		nc.Close()
	}

	logger.Warn("No valid Bootstrap server address discovered via DHCP6")
	return "", nil
}
