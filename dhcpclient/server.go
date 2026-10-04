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
	"encoding/binary"
	"fmt"
	"unicode/utf8"

	"fl-sztp-client/serveraddr"

	"github.com/sirupsen/logrus"
)

// serverFromOption accepts textual addresses and length-prefixed URI lists.
func serverFromOption(logger *logrus.Logger, iface string, option int, data []byte, present bool) string {
	entry := logger.WithFields(logrus.Fields{"interface": iface, "option": option})
	if !present {
		entry.Warn("Bootstrap server option not found in DHCP response")
		return ""
	}
	server, err := serverFromOptionData(data)
	if err != nil {
		entry.WithError(err).Warn("Invalid bootstrap server address in DHCP option")
		return ""
	}
	entry.Info("Bootstrap server address discovered via DHCP: ", server)
	return server
}

func serverFromOptionData(data []byte) (string, error) {
	uris, listErr := parseBootstrapURIList(data)
	if listErr != nil {
		if !utf8.Valid(data) {
			return "", fmt.Errorf("invalid bootstrap server option: URI list: %v; text address contains invalid UTF-8", listErr)
		}
		server, textErr := serveraddr.Normalize(string(data))
		if textErr != nil {
			return "", fmt.Errorf("invalid bootstrap server option: URI list: %v; text address: %w", listErr, textErr)
		}
		return server, nil
	}
	for _, uri := range uris {
		if !utf8.ValidString(uri) {
			continue
		}
		server, err := serveraddr.Normalize(uri)
		if err == nil {
			return server, nil
		}
	}
	return "", fmt.Errorf("bootstrap server URI list contains no usable address")
}

// parseBootstrapURIList validates every frame before returning any URI.
// Lengths count URI bytes and exclude the two-byte length field.
func parseBootstrapURIList(data []byte) ([]string, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("bootstrap server URI list is empty")
	}
	var uris []string
	for offset := 0; offset < len(data); {
		if len(data)-offset < 2 {
			return nil, fmt.Errorf("truncated URI length at byte %d", offset)
		}
		length := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		start := offset + 2
		if length > len(data)-start {
			return nil, fmt.Errorf("URI length %d at byte %d exceeds %d available bytes", length, offset, len(data)-start)
		}
		uris = append(uris, string(data[start:start+length]))
		offset = start + length
	}
	return uris, nil
}
