// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io"
	"log/slog"

	"fl-sztp-client/serveraddr"
	"fl-sztp-client/sztpclient"
)

type deviceDiscoveryFunc func(*slog.Logger, string) (sztpclient.DeviceInformation, error)

// reportWriter remembers output failures while still allowing selected probes to run.
type reportWriter struct {
	w   io.Writer
	err error
}

func (r *reportWriter) printf(format string, args ...any) {
	if r.err == nil {
		_, r.err = fmt.Fprintf(r.w, format, args...)
	}
}

func (r *reportWriter) value(label, value, source string) {
	if value == "" {
		r.printf("%s: not found / not configured\n", label)
		return
	}
	r.printf("%s: %q", label, value)
	if source != "" {
		r.printf(" (source: %q)", source)
	}
	r.printf("\n")
}

// runDiscoveryReport does not construct a bootstrap client, create directories,
// or contact the configured server. Each selected protocol gets one discovery round.
func runDiscoveryReport(out io.Writer, logger *slog.Logger, config *Config, device deviceDiscoveryFunc, v4, v6 discoveryFunc) int {
	r := &reportWriter{w: out}
	r.printf("Device information\n")
	info, deviceErr := device(logger, config.DeviceSerialNumber)
	r.value("Hardware serial number", info.HardwareSerialNumber, info.HardwareSerialSource)
	serialSource := info.SerialSource
	if serialSource == "override" && config.SerialSource != "" {
		serialSource = config.SerialSource
	}
	r.value("Authentication serial number", info.SerialNumber, serialSource)
	r.value("Hardware model", info.HardwareModel, "")
	r.value("OS name", info.OSName, "")
	r.value("OS version", info.OSVersion, "")
	if deviceErr != nil {
		r.printf("Device discovery error: %q\n", deviceErr.Error())
	}
	r.printf("\nCertificates (local inspection; authentication is not attempted)\n")
	reportCertificates(r, config)
	r.printf("\nDHCP discovery (address validation only)\n")
	probe := func(name string, discover discoveryFunc) string {
		logger.Info("Discovering server address", "protocol", name)
		server, err := discover(logger)
		if err != nil {
			r.printf("%s: discovery error: %q\n", name, err.Error())
			return ""
		}
		if server == "" {
			r.printf("%s: no accepted bootstrap server; see stderr for interface/option diagnostics\n", name)
		} else {
			r.printf("%s: accepted %q\n", name, server)
		}
		return server
	}
	var server4, server6 string
	if config.DiscoveryProtocol == discoverDHCP6 {
		r.printf("DHCPv4: skipped (not selected)\n")
	} else {
		server4 = probe("DHCPv4", v4)
	}
	switch {
	case config.DiscoveryProtocol == discoverDHCP4:
		r.printf("DHCPv6: skipped (not selected)\n")
	case config.DiscoverStopOnSuccess && server4 != "":
		r.printf("DHCPv6: skipped (bootstrap server already found via DHCPv4)\n")
	default:
		server6 = probe("DHCPv6", v6)
	}

	r.printf("\nBootstrap server selection (based on tested protocols)\n")
	if config.DiscoveryProtocol == discoverDHCP6 {
		r.printf("Without a configured server, normal bootstrap still tries DHCPv4 before DHCPv6; DHCPv4 was not tested.\n")
	}
	r.value("Configured server", config.Server, config.ServerSource)
	switch {
	case config.Server != "":
		r.value("Effective bootstrap server", config.Server, config.ServerSource)
	case server4 != "":
		r.value("Effective bootstrap server", server4, "DHCPv4")
	case server6 != "":
		r.value("Effective bootstrap server", server6, "DHCPv6")
	default:
		fallback, _ := serveraddr.Normalize(FALLBACK_SERVER)
		r.printf("No discovered server. Normal bootstrap retries discovery up to 10 rounds,\n")
		r.printf("waiting one minute between failed rounds, then uses fallback %q.\n", fallback)
	}
	code := 0
	if deviceErr != nil || info.SerialNumber == "" || (server6 == "" && server4 == "") {
		code = 1
	}
	r.printf("\nDiscovery test: ")
	if code == 0 {
		r.printf("PASS (accepted DHCP address and authentication serial number found)\n")
	} else {
		r.printf("FAIL (requires an accepted DHCP address and an authentication serial number)\n")
	}
	if r.err != nil {
		logger.Error("Failed to write discovery report", "error", r.err)
		return 1
	}
	return code
}
