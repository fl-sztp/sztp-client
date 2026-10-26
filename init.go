// SPDX-FileCopyrightText: 2025 Anssi Söderena <https://github.com/asode>
// SPDX-FileCopyrightText: 2025 Sandip Ghimire <https://github.com/sandip-ghimire>
// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode> (modifications)
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fl-sztp-client/serveraddr"

	"go.yaml.in/yaml/v3"
)

type discoveryProtocol uint8

const (
	discoverBoth discoveryProtocol = iota
	discoverDHCP4
	discoverDHCP6
)

type Config struct {
	DiscoverOnly          bool              `yaml:"-"`
	DiscoveryProtocol     discoveryProtocol `yaml:"-"`
	DiscoverStopOnSuccess bool              `yaml:"-"`
	ResetOnboarding       bool              `yaml:"-"`
	StateDir              string            `yaml:"state-dir"`
	configLoadErr         error
	ServerSource          string `yaml:"-"`
	SerialSource          string `yaml:"-"`
	Server                string `yaml:"server"`
	AllowInsecure         bool   `yaml:"insecure"`
	UseDebug              bool   `yaml:"debug"`
	TmpFolder             string `yaml:"tmp"`
	DeviceSerialNumber    string `yaml:"serial"`
	DevicePassword        string `yaml:"password"`
	DevicePrivateKey      string `yaml:"private-key"`
	DeviceEndEntityCert   string `yaml:"end-entity-cert"`
	BootstrapTrustAnchor  string `yaml:"trust-anchor-cert"`
}

func getConfigFromFile(filepath string) (*Config, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	config := &Config{}
	decoder := yaml.NewDecoder(file)
	if err := decoder.Decode(config); err != nil {
		return nil, err
	}

	return config, nil
}

// getConfig initializes the configuration by combining defaults, file contents, and flags.
func getConfig() (*Config, error) {
	// Define command line flags
	serverFlag := flag.String("server", "", "Specify the server address")
	allowInsecureFlag := flag.Bool("insecure", false, "Allow insecure connections (skip SSL certificate verification)")
	useDebugFlag := flag.Bool("debug", false, "Enable debug mode")
	discoverOnlyFlag := flag.Bool("discover-only", false, "Test DHCPv4 and DHCPv6 discovery and report device information and configured certificates without bootstrapping")
	discoverOnlyDHCP4Flag := flag.Bool("discover-only-dhcp4", false, "Test only DHCPv4 discovery and report device information and configured certificates without bootstrapping")
	discoverOnlyDHCP6Flag := flag.Bool("discover-only-dhcp6", false, "Test only DHCPv6 discovery and report device information and configured certificates without bootstrapping")
	discoverStopOnSuccessFlag := flag.Bool("discover-stop-on-success", false, "Stop DHCP probes after the first accepted bootstrap server address; requires a discover-only mode")
	resetOnboardingFlag := flag.Bool("reset-onboarding", false, "Remove the local onboarding completion state and exit without bootstrapping")
	stateDirFlag := flag.String("state-dir", "", "Persistent onboarding state directory (default: /var/lib/sztp-client; must be absolute)")
	tmpFolder := flag.String("tmp", "", "Specify the temporary folder (default/fallback: private folder with a UTC timestamp and random suffix under the system temporary directory)")
	deviceSerialNumber := flag.String("serial", "", "Specify the device serial number")
	devicePassword := flag.String("password", "", "Specify the device password")
	devicePrivateKey := flag.String("private-key", "", "Specify the device private key")
	deviceEndEntityCert := flag.String("end-entity-cert", "", "Specify the device end-entity certificate")
	bootstrapTrustAnchorCert := flag.String("trust-anchor-cert", "", "Specify the bootstrap trust-anchor certificate")
	configFile := flag.String("config", "./config.yaml", "Path to configuration file (YAML)")

	flag.Parse()
	configExplicit := false
	stateDirExplicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			configExplicit = true
		}
		if f.Name == "state-dir" {
			stateDirExplicit = true
		}
	})
	discoveryMode := ""
	protocol := discoverBoth
	for _, mode := range []struct {
		name     string
		enabled  bool
		protocol discoveryProtocol
	}{
		{"--discover-only", *discoverOnlyFlag, discoverBoth},
		{"--discover-only-dhcp4", *discoverOnlyDHCP4Flag, discoverDHCP4},
		{"--discover-only-dhcp6", *discoverOnlyDHCP6Flag, discoverDHCP6},
	} {
		if !mode.enabled {
			continue
		}
		if discoveryMode != "" {
			return nil, fmt.Errorf("%s and %s are mutually exclusive; select only one discover-only mode", discoveryMode, mode.name)
		}
		discoveryMode, protocol = mode.name, mode.protocol
	}
	if *discoverStopOnSuccessFlag && discoveryMode == "" {
		return nil, fmt.Errorf("--discover-stop-on-success requires --discover-only, --discover-only-dhcp4, or --discover-only-dhcp6")
	}
	if discoveryMode != "" && *resetOnboardingFlag {
		return nil, fmt.Errorf("--reset-onboarding cannot be combined with %s", discoveryMode)
	}

	// Load configuration from file if provided
	var config Config
	if *configFile != "" {
		fileConfig, err := getConfigFromFile(*configFile)
		if err != nil {
			loadErr := fmt.Errorf("error loading config file: %w", err)
			if configExplicit || !os.IsNotExist(err) {
				return nil, loadErr
			}
			// A missing implicit config is harmless for reset, diagnostics, or
			// an already onboarded device. Bootstrap still requires it.
			config.configLoadErr = loadErr
		}
		if fileConfig != nil {
			config = *fileConfig
			if config.Server != "" {
				config.ServerSource = "config: " + *configFile
			}
			if config.DeviceSerialNumber != "" {
				config.SerialSource = "config: " + *configFile
			}
		}
	}
	config.DiscoverOnly = discoveryMode != ""
	config.DiscoveryProtocol = protocol
	config.DiscoverStopOnSuccess = *discoverStopOnSuccessFlag
	config.ResetOnboarding = *resetOnboardingFlag
	if stateDirExplicit {
		config.StateDir = *stateDirFlag
		if config.StateDir == "" && !config.DiscoverOnly {
			return nil, fmt.Errorf("--state-dir must not be empty")
		}
	} else if config.StateDir == "" {
		config.StateDir = defaultStateDir
	}
	if !config.DiscoverOnly {
		if !filepath.IsAbs(config.StateDir) {
			return nil, fmt.Errorf("state directory must be an absolute path: %q", config.StateDir)
		}
		config.StateDir = filepath.Clean(config.StateDir)
	}

	// Override configuration with command-line flags if provided
	if *serverFlag != "" {
		config.Server = *serverFlag
		config.ServerSource = "CLI --server"
	}

	config.AllowInsecure = *allowInsecureFlag || config.AllowInsecure
	config.UseDebug = *useDebugFlag || config.UseDebug

	if *tmpFolder != "" {
		config.TmpFolder = *tmpFolder
	}
	if *deviceSerialNumber != "" {
		config.DeviceSerialNumber = *deviceSerialNumber
		config.SerialSource = "CLI --serial"
	}
	if *devicePassword != "" {
		config.DevicePassword = *devicePassword
	}
	if *devicePrivateKey != "" {
		config.DevicePrivateKey = *devicePrivateKey
	}
	if *deviceEndEntityCert != "" {
		config.DeviceEndEntityCert = *deviceEndEntityCert
	}
	if *bootstrapTrustAnchorCert != "" {
		config.BootstrapTrustAnchor = *bootstrapTrustAnchorCert
	}

	if config.DiscoverOnly {
		if err := normalizeConfigServer(&config); err != nil {
			return nil, err
		}
	}
	return &config, nil
}

func normalizeConfigServer(config *Config) error {
	config.Server = strings.TrimSpace(config.Server)
	if config.Server != "" {
		normalizedServer, err := serveraddr.Normalize(config.Server)
		if err != nil {
			return fmt.Errorf("invalid server address: %w", err)
		}
		config.Server = normalizedServer
	}
	return nil
}

func validateBootstrapConfig(config *Config) error {
	if config.configLoadErr != nil {
		return config.configLoadErr
	}
	if err := normalizeConfigServer(config); err != nil {
		return err
	}

	return validateReportingConfig(config)
}

func validateReportingConfig(config *Config) error {
	if config.configLoadErr != nil {
		return config.configLoadErr
	}
	// Ensure all mandatory values are set
	if config.DevicePassword == "" {
		return fmt.Errorf("device password is required")
	}
	if config.DevicePrivateKey == "" {
		return fmt.Errorf("device private key is required")
	}
	if config.DeviceEndEntityCert == "" {
		return fmt.Errorf("device end-entity certificate is required")
	}
	if config.BootstrapTrustAnchor == "" {
		return fmt.Errorf("bootstrap trust-anchor certificate is required")
	}

	return nil
}
