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
	"strconv"
	"strings"
	"time"

	"fl-sztp-client/serveraddr"
	"fl-sztp-client/sztpclient"

	"go.yaml.in/yaml/v3"
)

type discoveryProtocol uint8

const (
	discoverBoth discoveryProtocol = iota
	discoverDHCP4
	discoverDHCP6
)

type Config struct {
	DiscoverOnly              bool              `yaml:"-"`
	DiscoveryProtocol         discoveryProtocol `yaml:"-"`
	DiscoverStopOnSuccess     bool              `yaml:"-"`
	ResetOnboarding           bool              `yaml:"-"`
	StateDir                  string            `yaml:"state-dir"`
	configLoadErr             error
	configPath                string
	ServerSource              string                       `yaml:"-"`
	SerialSource              string                       `yaml:"-"`
	Server                    string                       `yaml:"server"`
	AllowInsecure             bool                         `yaml:"insecure"`
	UseDebug                  bool                         `yaml:"debug"`
	TmpFolder                 string                       `yaml:"tmp"`
	DeviceSerialNumber        string                       `yaml:"serial"`
	DevicePassword            string                       `yaml:"password"`
	DevicePrivateKey          string                       `yaml:"private-key"`
	DeviceEndEntityCert       string                       `yaml:"end-entity-cert"`
	BootstrapTrustAnchor      string                       `yaml:"trust-anchor-cert"`
	HostMetaTimeout           *string                      `yaml:"host-meta-timeout"`
	BootstrapTimeout          *string                      `yaml:"bootstrap-timeout"`
	ProgressTimeout           *string                      `yaml:"progress-timeout"`
	HostMetaMaxResponseBytes  *string                      `yaml:"host-meta-max-response-bytes"`
	BootstrapMaxResponseBytes *string                      `yaml:"bootstrap-max-response-bytes"`
	ProgressMaxResponseBytes  *string                      `yaml:"progress-max-response-bytes"`
	RequestLimits             sztpclient.HTTPRequestLimits `yaml:"-"`
}

// Keep raw option values until CLI overrides have been applied. In particular,
// an explicit zero must fail rather than silently select a library default.
type httpLimitOption struct {
	name    string
	raw     **string
	timeout *time.Duration
	bytes   *int64
}

func httpLimitOptions(c *Config) []httpLimitOption {
	return []httpLimitOption{
		{"host-meta-timeout", &c.HostMetaTimeout, &c.RequestLimits.HostMetaTimeout, nil},
		{"bootstrap-timeout", &c.BootstrapTimeout, &c.RequestLimits.BootstrapTimeout, nil},
		{"progress-timeout", &c.ProgressTimeout, &c.RequestLimits.ProgressTimeout, nil},
		{"host-meta-max-response-bytes", &c.HostMetaMaxResponseBytes, nil, &c.RequestLimits.HostMetaMaxResponseBytes},
		{"bootstrap-max-response-bytes", &c.BootstrapMaxResponseBytes, nil, &c.RequestLimits.BootstrapMaxResponseBytes},
		{"progress-max-response-bytes", &c.ProgressMaxResponseBytes, nil, &c.RequestLimits.ProgressMaxResponseBytes},
	}
}

func resolveHTTPRequestLimits(c *Config, overrides map[string]*string) error {
	c.RequestLimits = sztpclient.DefaultHTTPRequestLimits()
	for _, option := range httpLimitOptions(c) {
		if value, ok := overrides[option.name]; ok {
			*option.raw = value
		}
		if *option.raw == nil {
			continue
		}
		value := **option.raw
		if option.timeout != nil {
			duration, err := time.ParseDuration(value)
			if err != nil || duration <= 0 {
				return fmt.Errorf("%s must be a positive Go duration, such as 30s or 2m", option.name)
			}
			*option.timeout = duration
		} else {
			count, err := strconv.ParseInt(value, 10, 64)
			if err != nil || count <= 0 {
				return fmt.Errorf("%s must be a positive integer byte count", option.name)
			}
			*option.bytes = count
		}
	}
	return c.RequestLimits.Validate()
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
	return getConfigWithPaths([]string{"/etc/sztp-client/config.yaml", "./config.yaml"})
}

func getConfigWithPaths(defaultPaths []string) (*Config, error) {
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
	configFile := flag.String("config", "", "YAML configuration file (default: /etc/sztp-client/config.yaml, then ./config.yaml). An explicit empty value disables loading")
	defaultLimits := Config{RequestLimits: sztpclient.DefaultHTTPRequestLimits()}
	httpFlags := make(map[string]*string)
	for _, option := range httpLimitOptions(&defaultLimits) {
		var help string
		if option.timeout != nil {
			help = fmt.Sprintf("HTTP request deadline including response reading; positive Go duration (default: %s)", *option.timeout)
		} else {
			help = fmt.Sprintf("Maximum HTTP response body size after decompression; positive bytes (default: %d)", *option.bytes)
		}
		httpFlags[option.name] = flag.String(option.name, "", help)
	}

	flag.Parse()
	configExplicit := false
	stateDirExplicit := false
	httpOverrides := make(map[string]*string)
	flag.Visit(func(f *flag.Flag) {
		if value, ok := httpFlags[f.Name]; ok {
			httpOverrides[f.Name] = value
		}
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
	paths := defaultPaths
	if configExplicit {
		paths = nil
		if *configFile != "" {
			paths = []string{*configFile}
		}
	}
	for _, path := range paths {
		fileConfig, err := getConfigFromFile(path)
		if err != nil {
			loadErr := fmt.Errorf("error loading config file %q: %w", path, err)
			if configExplicit || !os.IsNotExist(err) {
				return nil, loadErr
			}
			// A missing implicit config is harmless for reset, diagnostics, or
			// an already onboarded device. Bootstrap still requires it.
			config.configLoadErr = loadErr
			continue
		}
		if fileConfig != nil {
			config = *fileConfig
			config.configPath = path
			if config.Server != "" {
				config.ServerSource = "config: " + path
			}
			if config.DeviceSerialNumber != "" {
				config.SerialSource = "config: " + path
			}
		}
		break
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
	if err := resolveHTTPRequestLimits(&config, httpOverrides); err != nil {
		return nil, err
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
	if err := config.RequestLimits.Validate(); err != nil {
		return err
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
