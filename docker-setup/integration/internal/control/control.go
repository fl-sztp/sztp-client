/*
SPDX-License-Identifier: Apache-2.0
Copyright (C) 2026 Anssi Söderena

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

// Package control defines the JSON contract shared by the DHCP fixture and driver.
package control

import (
	"encoding/hex"
	"fmt"
	"time"
)

// Reply describes one DHCP response, including deliberately malformed option data.
type Reply struct {
	Present    bool   `json:"present"`
	PayloadHex string `json:"payload_hex"`
	Fragments  []int  `json:"fragments"`
	Silent     bool   `json:"silent"`
}

// Scenario provides independent response sequences for DHCPv4 and DHCPv6.
type Scenario struct {
	V4 []Reply `json:"v4"`
	V6 []Reply `json:"v6"`
}

// Event records a request and the response observed by the fixture.
type Event struct {
	Protocol      int       `json:"protocol"`
	TransactionID string    `json:"transaction_id"`
	Requested     bool      `json:"requested"`
	RequestHex    string    `json:"request_hex"`
	ResponseHex   string    `json:"response_hex"`
	Error         string    `json:"error"`
	At            time.Time `json:"at"`
}

// PayloadBytes decodes the raw option data without interpreting URI contents.
func (r Reply) PayloadBytes() ([]byte, error) {
	return hex.DecodeString(r.PayloadHex)
}

// Validate checks response settings for protocol 4 or 6. An empty present option
// is valid; a missing option or silent reply cannot carry payload or fragments.
func (r Reply) Validate(protocol int) error {
	if protocol != 4 && protocol != 6 {
		return fmt.Errorf("unsupported protocol %d", protocol)
	}
	if r.Silent && r.Present {
		return fmt.Errorf("silent reply cannot have a present option")
	}
	if !r.Present && (r.PayloadHex != "" || len(r.Fragments) != 0) {
		return fmt.Errorf("missing option or silent reply cannot carry payload or fragments")
	}
	payload, err := r.PayloadBytes()
	if err != nil {
		return fmt.Errorf("invalid payload hex: %w", err)
	}
	if len(r.Fragments) == 0 {
		return nil
	}
	if protocol != 4 {
		return fmt.Errorf("fragments are supported only for IPv4")
	}
	remaining := len(payload)
	for i, size := range r.Fragments {
		if size < 1 || size > 255 {
			return fmt.Errorf("fragment %d size %d is outside 1..255", i, size)
		}
		if size > remaining {
			return fmt.Errorf("fragment sizes exceed payload length %d", len(payload))
		}
		remaining -= size
	}
	if remaining != 0 {
		return fmt.Errorf("fragment sizes do not total payload length %d", len(payload))
	}
	return nil
}

// Validate requires both protocol sequences and validates every reply. Reply
// indices in errors are zero-based, matching Go slices and JSON arrays.
func (s Scenario) Validate() error {
	for _, sequence := range []struct {
		protocol int
		replies  []Reply
	}{{4, s.V4}, {6, s.V6}} {
		if len(sequence.replies) == 0 {
			return fmt.Errorf("v%d reply sequence must not be empty", sequence.protocol)
		}
		for i, reply := range sequence.replies {
			if err := reply.Validate(sequence.protocol); err != nil {
				return fmt.Errorf("v%d reply %d: %w", sequence.protocol, i, err)
			}
		}
	}
	return nil
}
