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
	"flag"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server               string `yaml:"server"`
	AllowInsecure        bool   `yaml:"insecure"`
	UseDebug             bool   `yaml:"debug"`
	TmpFolder            string `yaml:"tmp"`
	DeviceSerialNumber   string `yaml:"serial"`
	DevicePassword       string `yaml:"password"`
	DevicePrivateKey     string `yaml:"private-key"`
	DeviceEndEntityCert  string `yaml:"end-entity-cert"`
	BootstrapTrustAnchor string `yaml:"trust-anchor-cert"`
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
	tmpFolder := flag.String("tmp", "", "Specify the temporary folder")
	deviceSerialNumber := flag.String("serial", "", "Specify the device serial number")
	devicePassword := flag.String("password", "", "Specify the device password")
	devicePrivateKey := flag.String("private-key", "", "Specify the device private key")
	deviceEndEntityCert := flag.String("end-entity-cert", "", "Specify the device end-entity certificate")
	bootstrapTrustAnchorCert := flag.String("trust-anchor-cert", "", "Specify the bootstrap trust-anchor certificate")
	configFile := flag.String("config", "./config.yaml", "Path to configuration file (YAML)")

	flag.Parse()

	// Load configuration from file if provided
	var config Config
	if *configFile != "" {
		fileConfig, err := getConfigFromFile(*configFile)
		if err != nil {
			return nil, fmt.Errorf("error loading config file: %v", err)
		}
		config = *fileConfig
	}

	// Override configuration with command-line flags if provided
	if *serverFlag != "" {
		config.Server = *serverFlag
	}

	config.AllowInsecure = *allowInsecureFlag || config.AllowInsecure
	config.UseDebug = *useDebugFlag || config.UseDebug

	if *tmpFolder != "" {
		config.TmpFolder = *tmpFolder
	}
	if *deviceSerialNumber != "" {
		config.DeviceSerialNumber = *deviceSerialNumber
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

	// Ensure all mandatory values are set
	if config.DevicePassword == "" {
		return nil, fmt.Errorf("device password is required")
	}
	if config.DevicePrivateKey == "" {
		return nil, fmt.Errorf("device private key is required")
	}
	if config.DeviceEndEntityCert == "" {
		return nil, fmt.Errorf("device end-entity certificate is required")
	}
	if config.BootstrapTrustAnchor == "" {
		return nil, fmt.Errorf("bootstrap trust-anchor certificate is required")
	}

	return &config, nil
}
