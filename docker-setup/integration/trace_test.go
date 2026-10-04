//go:build linux && dhcp_integration

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

package integration

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"net"
	"reflect"
	"testing"
	"time"

	"fl-sztp-client/docker-setup/integration/internal/control"
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv6"
)

type traceTransaction struct {
	protocol int
	id       string
}

func (d *integrationDriver) assertTrace(t *testing.T, c integrationCase, events []control.Event) {
	t.Helper()
	if len(events) == 0 {
		t.Fatal("discovery lacks real DHCP evidence")
	}
	assigned := make(map[traceTransaction]control.Reply)
	next := make(map[int]int)
	var order []int
	var first []control.Event
	var previous time.Time
	var server4 net.IP
	var server6 []byte
	for index, event := range events {
		if event.Protocol != 4 && event.Protocol != 6 {
			t.Fatalf("event %d has unknown protocol %d", index, event.Protocol)
		}
		length := 8
		if event.Protocol == 6 {
			length = 6
		}
		xid, err := hex.DecodeString(event.TransactionID)
		if err != nil || len(event.TransactionID) != length || hex.EncodeToString(xid) != event.TransactionID {
			t.Fatalf("event %d has invalid lowercase transaction ID %q", index, event.TransactionID)
		}
		request4, request6, err := decodeRequest(event)
		if err != nil {
			t.Fatalf("event %d request evidence: %v", index, err)
		}
		if !event.Requested {
			t.Fatalf("event %d did not request the bootstrap option", index)
		}
		if event.Error != "" {
			t.Fatalf("event %d fixture send failed: %s", index, event.Error)
		}
		if event.At.IsZero() || (!previous.IsZero() && event.At.Before(previous)) {
			t.Fatalf("event %d has missing or decreasing timestamp", index)
		}
		previous = event.At
		key := traceTransaction{event.Protocol, event.TransactionID}
		reply, retransmission := assigned[key]
		if !retransmission {
			sequence := c.scenario.V4
			if event.Protocol == 6 {
				sequence = c.scenario.V6
			}
			i := next[event.Protocol]
			if i >= len(sequence) {
				i = len(sequence) - 1
			} else {
				next[event.Protocol]++
			}
			reply = sequence[i]
			assigned[key] = reply
			order = append(order, event.Protocol)
			first = append(first, event)
		}
		// Retransmissions are collapsed only above for order. Every response is
		// independently decoded and compared to its transaction's configured reply.
		if reply.Silent {
			if event.ResponseHex != "" {
				t.Fatalf("silent event %d unexpectedly sent response bytes", index)
			}
			continue
		}
		wire, err := hex.DecodeString(event.ResponseHex)
		if err != nil || len(wire) == 0 {
			t.Fatalf("event %d lacks valid sent response bytes: %v", index, err)
		}
		payload, err := reply.PayloadBytes()
		if err != nil {
			t.Fatalf("decode configured payload: %v", err)
		}
		if event.Protocol == 4 {
			packet, err := dhcpv4.FromBytes(wire)
			if err != nil {
				t.Fatalf("event %d decode OFFER: %v", index, err)
			}
			if err := identity4(request4, packet); err != nil {
				t.Fatalf("event %d: %v", index, err)
			}
			identity := packet.ServerIdentifier()
			if identity.To4() == nil || identity.IsUnspecified() || identity.IsMulticast() || identity.Equal(net.IPv4bcast) {
				t.Fatalf("event %d OFFER lacks a valid server identifier", index)
			}
			if server4 != nil && !server4.Equal(identity) {
				t.Fatalf("event %d changed fixture IPv4 server identity", index)
			}
			server4 = identity
			code := dhcpv4.GenericOptionCode(143)
			if packet.Options.Has(code) != reply.Present || !bytes.Equal(packet.GetOneOption(code), payload) {
				t.Fatalf("event %d OFFER bootstrap presence/payload mismatch", index)
			}
			sizes, concatenated, err := fragments4(wire)
			if err != nil || !bytes.Equal(concatenated, payload) {
				t.Fatalf("event %d invalid raw option fragments: %v", index, err)
			}
			if (len(sizes) != 0) != reply.Present {
				t.Fatalf("event %d raw option presence mismatch", index)
			}
			for _, size := range sizes {
				if size == 0 && len(payload) != 0 {
					t.Fatalf("event %d empty fragment in nonempty payload", index)
				}
			}
			if len(reply.Fragments) != 0 && !reflect.DeepEqual(sizes, reply.Fragments) {
				t.Fatalf("event %d raw fragment lengths %v, want %v", index, sizes, reply.Fragments)
			}

		} else {
			parsed, err := dhcpv6.FromBytes(wire)
			if err != nil {
				t.Fatalf("event %d decode ADVERTISE: %v", index, err)
			}
			packet, ok := parsed.(*dhcpv6.Message)
			if !ok {
				t.Fatalf("event %d response is not a DHCPv6 message", index)
			}
			if err := identity6(request6, packet); err != nil {
				t.Fatalf("event %d: %v", index, err)
			}
			identity := packet.Options.ServerID()
			if identity == nil || len(identity.ToBytes()) == 0 {
				t.Fatalf("event %d ADVERTISE lacks a parseable server DUID", index)
			}
			if server6 != nil && !bytes.Equal(server6, identity.ToBytes()) {
				t.Fatalf("event %d changed fixture IPv6 server identity", index)
			}
			server6 = identity.ToBytes()
			option := packet.GetOneOption(dhcpv6.OptionCode(136))
			if (option != nil) != reply.Present || (option != nil && !bytes.Equal(option.ToBytes(), payload)) {
				t.Fatalf("event %d ADVERTISE bootstrap presence/payload mismatch", index)
			}
		}
	}
	if !reflect.DeepEqual(order, c.order) {
		t.Fatalf("distinct transaction protocol order %v, want %v", order, c.order)
	}
	if c.silentProtocol != 0 {
		var start time.Time
		observation := c.timeoutObserved
		for _, event := range first {
			if start.IsZero() && event.Protocol == c.silentProtocol {
				start = event.At
				continue
			}
			if !start.IsZero() && event.Protocol != c.silentProtocol {
				observation = event.At
				break
			}
		}
		if start.IsZero() || observation.IsZero() {
			t.Fatal("missing silent timeout observation")
		}
		// Allow one second for fixture receive scheduling; runtime is checked separately.
		if gap := observation.Sub(start); gap < 34*time.Second {
			t.Fatalf("silent DHCPv%d observation gap %s, require the real 35-second timeout", c.silentProtocol, gap)
		}
	}
	if c.minuteRetry {
		gap := first[2].At.Sub(first[1].At)
		if gap < 55*time.Second {
			t.Fatalf("next DHCP round started after %s, require at least 55 seconds", gap)
		}
	}
}

// Read raw option TLVs to check exact fragments, including a split URI length
// prefix; the DHCP library's parsed option map has already concatenated them.
func fragments4(wire []byte) ([]int, []byte, error) {
	if len(wire) < 240 || !bytes.Equal(wire[236:240], []byte{99, 130, 83, 99}) {
		return nil, nil, fmt.Errorf("invalid BOOTP header or DHCP magic cookie")
	}
	var sizes []int
	var payload []byte
	for i := 240; i < len(wire); {
		code := wire[i]
		i++
		if code == 255 {
			return sizes, payload, nil
		}
		if code == 0 {
			continue
		}
		if i >= len(wire) {
			return nil, nil, fmt.Errorf("truncated option length")
		}
		length := int(wire[i])
		i++
		if i+length > len(wire) {
			return nil, nil, fmt.Errorf("truncated option payload")
		}
		if code == 143 {
			sizes = append(sizes, length)
			payload = append(payload, wire[i:i+length]...)
		}
		i += length
	}
	return nil, nil, fmt.Errorf("missing DHCP end option")
}

// decodeRequest validates reconstructed library serialization, not an untouched datagram.
func decodeRequest(event control.Event) (*dhcpv4.DHCPv4, *dhcpv6.Message, error) {
	wire, err := hex.DecodeString(event.RequestHex)
	if err != nil || len(wire) == 0 {
		return nil, nil, fmt.Errorf("missing or malformed request hex")
	}
	if event.Protocol == 4 {
		m, err := dhcpv4.FromBytes(wire)
		if err != nil {
			return nil, nil, fmt.Errorf("decode DISCOVER: %w", err)
		}
		if m.OpCode != dhcpv4.OpcodeBootRequest || m.MessageType() != dhcpv4.MessageTypeDiscover || hex.EncodeToString(m.TransactionID[:]) != event.TransactionID {
			return nil, nil, fmt.Errorf("DISCOVER type or transaction mismatch")
		}
		if !bytes.Contains(m.GetOneOption(dhcpv4.OptionParameterRequestList), []byte{143}) {
			return nil, nil, fmt.Errorf("DISCOVER lacks requested bootstrap option")
		}
		return m, nil, nil
	}
	if event.Protocol != 6 {
		return nil, nil, fmt.Errorf("unknown protocol")
	}
	parsed, err := dhcpv6.FromBytes(wire)
	if err != nil {
		return nil, nil, fmt.Errorf("decode SOLICIT: %w", err)
	}
	m, ok := parsed.(*dhcpv6.Message)
	if !ok || m.MessageType != dhcpv6.MessageTypeSolicit || hex.EncodeToString(m.TransactionID[:]) != event.TransactionID {
		return nil, nil, fmt.Errorf("SOLICIT type or transaction mismatch")
	}
	if !m.IsOptionRequested(dhcpv6.OptionCode(136)) {
		return nil, nil, fmt.Errorf("SOLICIT lacks requested bootstrap option")
	}
	if m.Options.ClientID() == nil {
		return nil, nil, fmt.Errorf("SOLICIT lacks parseable client DUID")
	}
	return nil, m, nil
}

func identity4(request, response *dhcpv4.DHCPv4) error {
	if response.OpCode != dhcpv4.OpcodeBootReply || response.MessageType() != dhcpv4.MessageTypeOffer || response.TransactionID != request.TransactionID {
		return fmt.Errorf("OFFER type or transaction identity mismatch")
	}
	if response.HWType != request.HWType || !bytes.Equal(response.ClientHWAddr, request.ClientHWAddr) {
		return fmt.Errorf("OFFER hardware identity mismatch")
	}
	if response.Options.Has(dhcpv4.OptionClientIdentifier) != request.Options.Has(dhcpv4.OptionClientIdentifier) || !bytes.Equal(response.GetOneOption(dhcpv4.OptionClientIdentifier), request.GetOneOption(dhcpv4.OptionClientIdentifier)) {
		return fmt.Errorf("OFFER client identifier mismatch")
	}
	return nil
}

func identity6(request, response *dhcpv6.Message) error {
	if response.MessageType != dhcpv6.MessageTypeAdvertise || response.TransactionID != request.TransactionID {
		return fmt.Errorf("ADVERTISE type or transaction identity mismatch")
	}
	client, original := response.Options.ClientID(), request.Options.ClientID()
	if client == nil || original == nil || !bytes.Equal(client.ToBytes(), original.ToBytes()) {
		return fmt.Errorf("ADVERTISE client DUID mismatch")
	}
	return nil
}
