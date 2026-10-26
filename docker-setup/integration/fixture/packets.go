//go:build linux

// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"net"

	"fl-sztp-client/docker-setup/integration/internal/control"
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv6"
)

var option4 = dhcpv4.GenericOptionCode(143)

const option6 = dhcpv6.OptionCode(136)

func offerBytes(request *dhcpv4.DHCPv4, serverIP net.IP, reply control.Reply) ([]byte, error) {
	if err := reply.Validate(4); err != nil {
		return nil, err
	}
	if reply.Silent {
		return nil, nil
	}
	m, err := dhcpv4.NewReplyFromRequest(request,
		dhcpv4.WithMessageType(dhcpv4.MessageTypeOffer),
		dhcpv4.WithYourIP(net.IPv4(192, 0, 2, 10)),
		dhcpv4.WithOption(dhcpv4.OptServerIdentifier(serverIP)))
	if err != nil {
		return nil, err
	}
	payload, err := reply.PayloadBytes()
	if err != nil {
		return nil, err
	}
	if len(reply.Fragments) == 0 {
		if reply.Present {
			m.UpdateOption(dhcpv4.OptGeneric(option4, payload))
		}
		return m.ToBytes(), nil
	}
	// Build the packet without option 143 so the library cannot merge or
	// automatically re-split the explicitly requested RFC 3396 fragments.
	wire := m.ToBytes()
	end, err := option4End(wire)
	if err != nil {
		return nil, err
	}
	wire = wire[:end]
	pos := 0
	for _, size := range reply.Fragments {
		wire = append(wire, option4.Code(), byte(size))
		wire = append(wire, payload[pos:pos+size]...)
		pos += size
	}
	wire = append(wire, 255)
	for len(wire) < 300 {
		wire = append(wire, 0)
	}
	return wire, nil
}

func option4End(wire []byte) (int, error) {
	for i := 240; i < len(wire); {
		code := wire[i]
		if code == 255 {
			return i, nil
		}
		i++
		if code == 0 {
			continue
		}
		if i >= len(wire) {
			break
		}
		i += 1 + int(wire[i])
	}
	return 0, fmt.Errorf("generated DHCPv4 packet has no end option")
}

func advertiseBytes(request *dhcpv6.Message, serverID dhcpv6.DUID, reply control.Reply) ([]byte, error) {
	if err := reply.Validate(6); err != nil {
		return nil, err
	}
	if reply.Silent {
		return nil, nil
	}
	m, err := dhcpv6.NewAdvertiseFromSolicit(request, dhcpv6.WithServerID(serverID))
	if err != nil {
		return nil, err
	}
	if reply.Present {
		payload, err := reply.PayloadBytes()
		if err != nil {
			return nil, err
		}
		m.AddOption(&dhcpv6.OptionGeneric{OptionCode: option6, OptionData: payload})
	}
	return m.ToBytes(), nil
}
