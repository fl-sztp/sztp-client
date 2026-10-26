//go:build linux
// +build linux

// SPDX-FileCopyrightText: 2025 Anssi Söderena <https://github.com/asode>
// SPDX-FileCopyrightText: 2025 Sandip Ghimire <https://github.com/sandip-ghimire>
// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode> (modifications)
// SPDX-License-Identifier: Apache-2.0

package dhcpclient

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/dhcpv6/nclient6"
)

// GetServerFromDHCP6 returns the first usable bootstrap server as a normalized
// HTTPS origin, or an empty string when no usable server is discovered.
func GetServerFromDHCP6(logger *slog.Logger) (string, error) {

	// TODO: support for multiple servers?

	interfaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("get network interfaces for bootstrap server discovery via DHCP6: %w", err)
	}
	for _, iface := range interfaces {
		entry := logger.With("protocol", "DHCPv6", "interface", iface.Name)
		// Skip loopback interfaces
		if iface.Flags&net.FlagLoopback != 0 {
			entry.Debug("Skipping loopback interface")
			continue
		}
		if iface.Flags&net.FlagRunning == 0 {
			entry.Debug("Skipping inactive interface")
			continue
		}
		entry.Debug("Discovering bootstrap server on interface")
		nc, err := nclient6.New(iface.Name)
		if err != nil {
			entry.Warn("Failed to create DHCP client", "error", err)
			continue
		}
		advertise, err := nc.Solicit(context.Background(), dhcpv6.WithRequestedOptions(dhcpv6.OptionCode(136)))
		if err != nil {
			entry.Warn("Failed to discover bootstrap server", "error", err)
			nc.Close()
			continue
		}
		entry.Debug("DHCP response received", "response", advertise.Summary())

		optionData := advertise.GetOneOption(dhcpv6.OptionCode(136))
		var optionBytes []byte
		if optionData != nil {
			optionBytes = optionData.ToBytes()
		}
		server := serverFromOption(logger.With("protocol", "DHCPv6"), iface.Name, 136, optionBytes, optionData != nil)
		nc.Close()
		if server != "" {
			return server, nil
		}
	}

	logger.Warn("No valid bootstrap server discovered", "protocol", "DHCPv6")
	return "", nil
}
