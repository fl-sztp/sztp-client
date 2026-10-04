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
	"time"

	"github.com/sirupsen/logrus"
)

type discoveryFunc func(*logrus.Logger) (string, error)

func discoverServerOnce(logger *logrus.Logger, v6, v4 discoveryFunc) string {
	logger.Info("Discovering server address via DHCP6...")
	server, err := v6(logger)
	if err != nil {
		logger.Warn("Failed to discover server address via DHCP6: ", err)
	} else if server != "" {
		return server
	}

	logger.Info("Discovering server address via DHCP4...")
	server, err = v4(logger)
	if err != nil {
		logger.Warn("Failed to discover server address via DHCP4: ", err)
		return ""
	}
	return server
}

func discoverServerWithRetry(logger *logrus.Logger, v6, v4 discoveryFunc, attempts int, delay time.Duration, sleep func(time.Duration)) string {
	for attempt := 1; attempt <= attempts; attempt++ {
		if server := discoverServerOnce(logger, v6, v4); server != "" {
			return server
		}
		logger.Errorf("Failed to discover server address via DHCP (attempt %d/%d)", attempt, attempts)
		if attempt < attempts {
			logger.Warnf("Retrying DHCP discovery in %s...", delay)
			sleep(delay)
		}
	}
	return ""
}
