//go:build linux

// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"fl-sztp-client/docker-setup/integration/internal/control"
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/iana"
)

const writeTimeout = time.Second

type transaction struct {
	protocol int
	id       string
}

type fixture struct {
	mu       sync.Mutex
	scenario control.Scenario
	next     map[int]int
	replies  map[transaction]control.Reply
	events   []control.Event
	serving  map[int]bool
	serverIP net.IP
	serverID dhcpv6.DUID
}

func newFixture(ip net.IP, mac net.HardwareAddr) *fixture {
	f := &fixture{
		serverIP: append(net.IP(nil), ip...),
		serverID: &dhcpv6.DUIDLL{HWType: iana.HWTypeEthernet, LinkLayerAddr: append(net.HardwareAddr(nil), mac...)},
		serving:  make(map[int]bool),
	}
	f.reset(control.Scenario{V4: []control.Reply{{}}, V6: []control.Reply{{}}})
	return f
}

// reset owns the scenario and atomically starts a fresh transaction history.
func (f *fixture) reset(scenario control.Scenario) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scenario = control.Scenario{V4: cloneReplies(scenario.V4), V6: cloneReplies(scenario.V6)}
	f.next = make(map[int]int)
	f.replies = make(map[transaction]control.Reply)
	f.events = make([]control.Event, 0)
}

func cloneReplies(replies []control.Reply) []control.Reply {
	cloned := append([]control.Reply(nil), replies...)
	for i := range cloned {
		cloned[i].Fragments = append([]int(nil), cloned[i].Fragments...)
	}
	return cloned
}

// selectReply is called with mu held. Retransmissions reuse their first reply.
func (f *fixture) selectReply(protocol int, id string) control.Reply {
	key := transaction{protocol, id}
	if reply, ok := f.replies[key]; ok {
		return reply
	}
	sequence := f.scenario.V4
	if protocol == 6 {
		sequence = f.scenario.V6
	}
	i := f.next[protocol]
	if i >= len(sequence) {
		i = len(sequence) - 1
	} else {
		f.next[protocol]++
	}
	reply := sequence[i]
	f.replies[key] = reply
	return reply
}

func (f *fixture) handle4(conn net.PacketConn, peer net.Addr, request *dhcpv4.DHCPv4) {
	if request == nil || request.MessageType() != dhcpv4.MessageTypeDiscover {
		return
	}
	// IsOptionRequested treats an absent PRL as requesting every option. The
	// trace records literal option-143 presence in the received PRL instead.
	requested := bytes.Contains(request.GetOneOption(dhcpv4.OptionParameterRequestList), []byte{option4.Code()})
	f.respond(conn, peer, 4, hex.EncodeToString(request.TransactionID[:]), requested, request.ToBytes(), func(reply control.Reply) ([]byte, error) {
		return offerBytes(request, f.serverIP, reply)
	})
}

func (f *fixture) handle6(conn net.PacketConn, peer net.Addr, packet dhcpv6.DHCPv6) {
	request, ok := packet.(*dhcpv6.Message)
	if !ok || request == nil || request.MessageType != dhcpv6.MessageTypeSolicit {
		return
	}
	f.respond(conn, peer, 6, hex.EncodeToString(request.TransactionID[:]), request.IsOptionRequested(option6), request.ToBytes(), func(reply control.Reply) ([]byte, error) {
		return advertiseBytes(request, f.serverID, reply)
	})
}

func (f *fixture) respond(conn net.PacketConn, peer net.Addr, protocol int, id string, requested bool, request []byte, build func(control.Reply) ([]byte, error)) {
	// Hold the mutex through the bounded write so reset cannot mix generations,
	// and concurrent handlers cannot replace a shared socket's write deadline.
	f.mu.Lock()
	defer f.mu.Unlock()
	event := control.Event{Protocol: protocol, TransactionID: id, Requested: requested, RequestHex: hex.EncodeToString(request), At: time.Now().UTC()}
	reply := f.selectReply(protocol, id)
	if !reply.Silent {
		wire, err := build(reply)
		if err == nil {
			err = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
		}
		if err == nil {
			var n int
			n, err = conn.WriteTo(wire, peer)
			if n >= 0 && n <= len(wire) {
				event.ResponseHex = hex.EncodeToString(wire[:n])
			}
			if err == nil && n != len(wire) {
				err = fmt.Errorf("%w: wrote %d of %d bytes", io.ErrShortWrite, n, len(wire))
			}
		}
		if err != nil {
			event.Error = err.Error()
		}
	}
	f.events = append(f.events, event)
}

func (f *fixture) setServing(protocol int, serving bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.serving[protocol] = serving
}
