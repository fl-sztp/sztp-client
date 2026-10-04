//go:build linux
// +build linux

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

package dhcpclient

import (
	"context"
	"fmt"
	"net"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/dhcpv6/nclient6"
	"github.com/sirupsen/logrus"
)

// GetServerFromDHCP6 returns the first usable bootstrap server as a normalized
// HTTPS origin, or an empty string when no usable server is discovered.
func GetServerFromDHCP6(logger *logrus.Logger) (string, error) {

	// TODO: support for multiple servers?

	interfaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("get network interfaces for bootstrap server discovery via DHCP6: %w", err)
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
			logger.WithField("interface", iface.Name).WithError(err).Warn("Failed to create DHCPv6 client for discovering the Bootstrap server via DHCP6")
			continue
		}
		advertise, err := nc.Solicit(context.Background(), dhcpv6.WithRequestedOptions(dhcpv6.OptionCode(136)))
		if err != nil {
			logger.WithField("interface", iface.Name).WithError(err).Warn("Failed to solicit the Bootstrap server via DHCP6")
			nc.Close()
			continue
		}
		logger.Debug("DHCP6 advertise received: ", advertise.Summary())

		optionData := advertise.GetOneOption(dhcpv6.OptionCode(136))
		var optionBytes []byte
		if optionData != nil {
			optionBytes = optionData.ToBytes()
		}
		server := serverFromOption(logger, iface.Name, 136, optionBytes, optionData != nil)
		nc.Close()
		if server != "" {
			return server, nil
		}
	}

	logger.Warn("No valid Bootstrap server address discovered via DHCP6")
	return "", nil
}
