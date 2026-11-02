//go:build linux

// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"fl-sztp-client/docker-tests/integration/internal/control"
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv6"
)

var testMAC = net.HardwareAddr{2, 0, 0, 0, 0, 7}
var testIP = net.IPv4(172, 28, 0, 2)

func discovery(t *testing.T) *dhcpv4.DHCPv4 {
	t.Helper()
	m, err := dhcpv4.NewDiscovery(testMAC,
		dhcpv4.WithRequestedOptions(dhcpv4.GenericOptionCode(143)),
		dhcpv4.WithOption(dhcpv4.OptGeneric(dhcpv4.OptionClientIdentifier, []byte{1, 2, 3, 4})))
	if err != nil {
		t.Fatal(err)
	}
	m.TransactionID = dhcpv4.TransactionID{1, 2, 3, 4}
	return m
}

func solicit(t *testing.T) *dhcpv6.Message {
	t.Helper()
	m, err := dhcpv6.NewSolicit(testMAC, dhcpv6.WithRequestedOptions(dhcpv6.OptionCode(136)))
	if err != nil {
		t.Fatal(err)
	}
	m.TransactionID = dhcpv6.TransactionID{1, 2, 3}
	return m
}

// option4Fragments reads wire TLVs rather than the library's concatenated map.
func option4Fragments(t *testing.T, wire []byte) [][]byte {
	t.Helper()
	var fragments [][]byte
	for i := 240; i < len(wire); {
		code := wire[i]
		i++
		if code == 255 {
			return fragments
		}
		if code == 0 {
			continue
		}
		if i >= len(wire) {
			t.Fatal("truncated option length")
		}
		n := int(wire[i])
		i++
		if i+n > len(wire) {
			t.Fatal("truncated option data")
		}
		if code == 143 {
			fragments = append(fragments, append([]byte{}, wire[i:i+n]...))
		}
		i += n
	}
	t.Fatal("no DHCP end option")
	return nil
}

func TestOfferWire(t *testing.T) {
	request := discovery(t)
	long := bytes.Repeat([]byte{0xff, 0, 0x7f}, 100)
	for _, tc := range []struct {
		name  string
		reply control.Reply
		sizes []int
	}{
		{"absent", control.Reply{}, nil},
		{"empty", control.Reply{Present: true}, []int{0}},
		{"raw", control.Reply{Present: true, PayloadHex: "0003ff808100"}, []int{6}},
		{"explicit fragments", control.Reply{Present: true, PayloadHex: "0003ff808100", Fragments: []int{1, 2, 3}}, []int{1, 2, 3}},
		{"automatic fragments", control.Reply{Present: true, PayloadHex: hex.EncodeToString(long)}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire, err := offerBytes(request, testIP, tc.reply)
			if err != nil {
				t.Fatal(err)
			}
			m, err := dhcpv4.FromBytes(wire)
			if err != nil {
				t.Fatal(err)
			}
			if m.MessageType() != dhcpv4.MessageTypeOffer || m.OpCode != dhcpv4.OpcodeBootReply {
				t.Fatalf("not an offer: %v", m)
			}
			if m.TransactionID != request.TransactionID || !bytes.Equal(m.ClientHWAddr, request.ClientHWAddr) {
				t.Fatal("request identity changed")
			}
			if !bytes.Equal(m.Options.Get(dhcpv4.OptionClientIdentifier), request.Options.Get(dhcpv4.OptionClientIdentifier)) {
				t.Fatal("client identifier changed")
			}
			if !m.ServerIdentifier().Equal(testIP) || !m.YourIPAddr.Equal(net.IPv4(192, 0, 2, 10)) {
				t.Fatal("incorrect server or dummy lease address")
			}
			if m.Options.Has(dhcpv4.GenericOptionCode(143)) != tc.reply.Present {
				t.Fatal("option presence changed")
			}
			payload, _ := tc.reply.PayloadBytes()
			if !bytes.Equal(m.Options.Get(dhcpv4.GenericOptionCode(143)), payload) {
				t.Fatal("raw option payload changed")
			}
			fragments := option4Fragments(t, wire)
			if (len(fragments) != 0) != tc.reply.Present {
				t.Fatal("raw option presence changed")
			}
			var sizes []int
			for _, fragment := range fragments {
				if len(fragment) > 255 || (len(fragment) == 0 && len(payload) != 0) {
					t.Fatal("invalid fragment length")
				}
				sizes = append(sizes, len(fragment))
			}
			if len(tc.reply.Fragments) != 0 && !reflect.DeepEqual(sizes, tc.sizes) {
				t.Fatalf("fragment lengths %v, want %v", sizes, tc.sizes)
			}
			if !bytes.Equal(bytes.Join(fragments, nil), payload) {
				t.Fatal("fragment bytes changed")
			}
		})
	}
}

func TestAdvertiseWire(t *testing.T) {
	request := solicit(t)
	f := newFixture(testIP, testMAC)
	for _, reply := range []control.Reply{{}, {Present: true}, {Present: true, PayloadHex: "0003ff808100"}} {
		wire, err := advertiseBytes(request, f.serverID, reply)
		if err != nil {
			t.Fatal(err)
		}
		packet, err := dhcpv6.FromBytes(wire)
		if err != nil {
			t.Fatal(err)
		}
		m, ok := packet.(*dhcpv6.Message)
		if !ok || m.MessageType != dhcpv6.MessageTypeAdvertise || m.TransactionID != request.TransactionID {
			t.Fatal("advertise type or transaction changed")
		}
		if !bytes.Equal(m.Options.ClientID().ToBytes(), request.Options.ClientID().ToBytes()) || !bytes.Equal(m.Options.ServerID().ToBytes(), f.serverID.ToBytes()) {
			t.Fatal("client or server DUID changed")
		}
		option := m.GetOneOption(dhcpv6.OptionCode(136))
		if (option != nil) != reply.Present {
			t.Fatal("option presence changed")
		}
		payload, _ := reply.PayloadBytes()
		if option != nil && !bytes.Equal(option.ToBytes(), payload) {
			t.Fatal("raw option payload changed")
		}
	}
}

type captureConn struct {
	packets       [][]byte
	peers         []net.Addr
	deadline      time.Time
	deadlineError error
	writeError    error
	short         bool
}

func (c *captureConn) WriteTo(p []byte, peer net.Addr) (int, error) {
	c.peers = append(c.peers, peer)
	if c.writeError != nil {
		return 0, c.writeError
	}
	n := len(p)
	if c.short {
		n--
	}
	c.packets = append(c.packets, append([]byte(nil), p[:n]...))
	return n, nil
}
func (*captureConn) ReadFrom([]byte) (int, net.Addr, error) { return 0, nil, io.EOF }
func (*captureConn) Close() error                           { return nil }
func (*captureConn) LocalAddr() net.Addr                    { return &net.UDPAddr{} }
func (*captureConn) SetDeadline(time.Time) error            { return nil }
func (*captureConn) SetReadDeadline(time.Time) error        { return nil }
func (c *captureConn) SetWriteDeadline(at time.Time) error  { c.deadline = at; return c.deadlineError }

func putScenario(t *testing.T, f *fixture, body string, status int) {
	t.Helper()
	w := httptest.NewRecorder()
	f.routes().ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/scenario", strings.NewReader(body)))
	if w.Code != status {
		t.Fatalf("PUT status %d, want %d: %s", w.Code, status, w.Body.String())
	}
}

func readEvents(t *testing.T, f *fixture) []control.Event {
	t.Helper()
	w := httptest.NewRecorder()
	f.routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/events", nil))
	var events []control.Event
	if err := json.Unmarshal(w.Body.Bytes(), &events); err != nil {
		t.Fatal(err)
	}
	return events
}

func TestSequencesRetransmissionsAndReset(t *testing.T) {
	f := newFixture(testIP, testMAC)
	putScenario(t, f, `{"v4":[{"present":true,"payload_hex":"01"},{"present":true,"payload_hex":"02"}],"v6":[{"silent":true},{"present":true,"payload_hex":"03"}]}`, http.StatusNoContent)
	c := &captureConn{}
	peer := &net.UDPAddr{IP: net.IPv4bcast, Port: 68}
	req := discovery(t)
	for _, id := range []byte{1, 1, 2, 3, 1} {
		req.TransactionID[0] = id
		f.handle4(c, peer, req)
	}
	for i, want := range []byte{1, 1, 2, 2, 1} {
		m, err := dhcpv4.FromBytes(c.packets[i])
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(m.GetOneOption(dhcpv4.GenericOptionCode(143)), []byte{want}) {
			t.Fatalf("packet %d advanced sequence incorrectly", i)
		}
		if c.peers[i] != peer {
			t.Fatal("handler changed server-provided peer")
		}
	}
	if c.deadline.IsZero() || time.Until(c.deadline) > 2*time.Second {
		t.Fatal("write deadline is unbounded")
	}
	request6 := solicit(t)
	f.handle6(c, peer, request6)
	f.handle6(c, peer, request6)
	if len(c.packets) != 5 {
		t.Fatal("silent reply wrote a packet")
	}
	request6.TransactionID[0] = 2
	f.handle6(c, peer, request6)
	request6.TransactionID[0] = 3
	f.handle6(c, peer, request6)
	for _, wire := range c.packets[5:] {
		m, err := dhcpv6.FromBytes(wire)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(m.GetOneOption(dhcpv6.OptionCode(136)).ToBytes(), []byte{3}) {
			t.Fatal("v6 sequence or protocol isolation failed")
		}
	}
	events := readEvents(t, f)
	if len(events) != 9 {
		t.Fatalf("%d events, want 9", len(events))
	}
	for i, event := range events {
		if event.RequestHex == "" || !event.Requested || event.At.IsZero() || event.Error != "" {
			t.Fatalf("invalid request trace: %+v", event)
		}
		if i == 5 || i == 6 {
			if event.ResponseHex != "" {
				t.Fatal("silent response bytes recorded")
			}
			continue
		}
		if i < 5 && event.TransactionID != []string{"01020304", "01020304", "02020304", "03020304", "01020304"}[i] {
			t.Fatal("IPv4 transaction trace changed")
		}
		packetIndex := i
		if i > 6 {
			packetIndex -= 2
		}
		if event.ResponseHex != hex.EncodeToString(c.packets[packetIndex]) {
			t.Fatal("response trace differs from sent packet")
		}
	}
	putScenario(t, f, `{"v4":[{"present":true,"payload_hex":"04"}],"v6":[{}]}`, http.StatusNoContent)
	if len(readEvents(t, f)) != 0 {
		t.Fatal("events not cleared on reset")
	}
	req.TransactionID[0] = 1
	f.handle4(c, peer, req)
	m, err := dhcpv4.FromBytes(c.packets[len(c.packets)-1])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(m.GetOneOption(dhcpv4.GenericOptionCode(143)), []byte{4}) {
		t.Fatal("transaction cache not cleared on reset")
	}
	request6.TransactionID[0] = 1
	f.handle6(c, peer, request6)
	packet, err := dhcpv6.FromBytes(c.packets[len(c.packets)-1])
	if err != nil {
		t.Fatal(err)
	}
	if packet.GetOneOption(dhcpv6.OptionCode(136)) != nil {
		t.Fatal("IPv6 transaction cache not cleared on reset")
	}
}

func TestSilentIPv4(t *testing.T) {
	f := newFixture(testIP, testMAC)
	putScenario(t, f, `{"v4":[{"silent":true}],"v6":[{}]}`, http.StatusNoContent)
	c := &captureConn{}
	f.handle4(c, &net.UDPAddr{}, discovery(t))
	events := readEvents(t, f)
	if len(c.packets) != 0 || len(events) != 1 || events[0].RequestHex == "" || !events[0].Requested || events[0].Error != "" || events[0].ResponseHex != "" {
		t.Fatalf("incorrect silent IPv4 behavior: %+v", events)
	}
}

func TestControlRejectsInvalidAtomically(t *testing.T) {
	f := newFixture(testIP, testMAC)
	putScenario(t, f, `{"v4":[{"present":true,"payload_hex":"01"},{"present":true,"payload_hex":"02"}],"v6":[{}]}`, http.StatusNoContent)
	c := &captureConn{}
	req := discovery(t)
	f.handle4(c, &net.UDPAddr{}, req)
	for _, body := range []string{`{`, `{"v4":[],"v6":[{}]}`, `{"v4":[{"payload_hex":"ff"}],"v6":[{}]}`, `{"v4":[{}],"v6":[{}]} {}`, `{"v4":[{}],"v6":[{}],"typo":true}`} {
		putScenario(t, f, body, http.StatusBadRequest)
		if len(readEvents(t, f)) != 1 {
			t.Fatal("invalid scenario cleared events")
		}
	}
	req.TransactionID[0] = 2
	f.handle4(c, &net.UDPAddr{}, req)
	m, err := dhcpv4.FromBytes(c.packets[1])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(m.GetOneOption(dhcpv4.GenericOptionCode(143)), []byte{2}) {
		t.Fatal("invalid scenario changed sequence")
	}
}

func TestTraceErrorsAndRequestedPresence(t *testing.T) {
	for _, protocol := range []int{4, 6} {
		for _, tc := range []struct {
			name     string
			conn     captureConn
			contains string
		}{
			{"write", captureConn{writeError: errors.New("write failed")}, "write failed"},
			{"short", captureConn{short: true}, "short write"},
			{"deadline", captureConn{deadlineError: errors.New("deadline failed")}, "deadline failed"},
			{"success", captureConn{}, ""},
		} {
			t.Run(tc.name+string(rune('0'+protocol)), func(t *testing.T) {
				f := newFixture(testIP, testMAC)
				if protocol == 4 {
					req := discovery(t)
					req.DeleteOption(dhcpv4.OptionParameterRequestList)
					f.handle4(&tc.conn, &net.UDPAddr{}, req)
				} else {
					req := solicit(t)
					req.Options.Del(dhcpv6.OptionORO)
					f.handle6(&tc.conn, &net.UDPAddr{}, req)
				}
				events := readEvents(t, f)
				if len(events) != 1 || events[0].RequestHex == "" || events[0].Protocol != protocol || events[0].Requested || !strings.Contains(events[0].Error, tc.contains) {
					t.Fatalf("incorrect event: %+v", events)
				}
				if tc.contains == "" && events[0].Error != "" {
					t.Fatal(events[0].Error)
				}
				if len(tc.conn.packets) == 0 {
					if events[0].ResponseHex != "" {
						t.Fatal("failed write recorded unsent bytes")
					}
				} else if events[0].ResponseHex != hex.EncodeToString(tc.conn.packets[0]) {
					t.Fatal("trace did not retain actually written bytes")
				}
			})
		}
	}
}

func TestRequestedOptionTraceWireLists(t *testing.T) {
	for _, tc := range []struct {
		name      string
		list      []byte
		present   bool
		requested bool
	}{
		{"absent", nil, false, false},
		{"empty", []byte{}, true, false},
		{"unrelated", []byte{1, 3}, true, false},
		{"target", []byte{143}, true, true},
		{"mixed", []byte{1, 143, 3}, true, true},
	} {
		for _, protocol := range []int{4, 6} {
			t.Run(tc.name+string(rune('0'+protocol)), func(t *testing.T) {
				f := newFixture(testIP, testMAC)
				c := &captureConn{}
				if protocol == 4 {
					request := discovery(t)
					request.DeleteOption(dhcpv4.OptionParameterRequestList)
					if tc.present {
						request.UpdateOption(dhcpv4.OptGeneric(dhcpv4.OptionParameterRequestList, tc.list))
					}
					parsed, err := dhcpv4.FromBytes(request.ToBytes())
					if err != nil {
						t.Fatal(err)
					}
					f.handle4(c, &net.UDPAddr{}, parsed)
				} else {
					request := solicit(t)
					request.Options.Del(dhcpv6.OptionORO)
					if tc.present {
						var list []byte
						for _, code := range tc.list {
							if code == 143 {
								code = 136
							}
							list = append(list, 0, code)
						}
						request.AddOption(&dhcpv6.OptionGeneric{OptionCode: dhcpv6.OptionORO, OptionData: list})
					}
					parsed, err := dhcpv6.FromBytes(request.ToBytes())
					if err != nil {
						t.Fatal(err)
					}
					f.handle6(c, &net.UDPAddr{}, parsed)
				}
				events := readEvents(t, f)
				if len(events) != 1 || events[0].Requested != tc.requested {
					t.Fatalf("request trace %+v, want requested=%t", events, tc.requested)
				}
			})
		}
	}
}

func TestHealthRequiresBothServingSockets(t *testing.T) {
	f := newFixture(testIP, testMAC)
	for _, ready := range []struct {
		protocol int
		status   int
	}{{0, http.StatusServiceUnavailable}, {4, http.StatusServiceUnavailable}, {6, http.StatusOK}} {
		if ready.protocol != 0 {
			if ready.protocol == 4 {
				(&servingLogger4{f}).Printf("Ready to handle requests")
			} else {
				(&servingLogger6{f}).Printf("Ready to handle requests")
			}
		}
		w := httptest.NewRecorder()
		f.routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
		if w.Code != ready.status {
			t.Fatalf("health status %d, want %d", w.Code, ready.status)
		}
	}
	f.setServing(4, false)
	w := httptest.NewRecorder()
	f.routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("health stayed ready after serving stopped")
	}
}

func TestRequestSerializationEvidence(t *testing.T) {
	for _, protocol := range []int{4, 6} {
		for _, silent := range []bool{false, true} {
			f := newFixture(testIP, testMAC)
			f.reset(control.Scenario{V4: []control.Reply{{Silent: silent}}, V6: []control.Reply{{Silent: silent}}})
			conn := &captureConn{writeError: errors.New("send failure")}
			var wire []byte
			if protocol == 4 {
				request := discovery(t)
				wire = request.ToBytes()
				f.handle4(conn, &net.UDPAddr{}, request)
			} else {
				request := solicit(t)
				wire = request.ToBytes()
				f.handle6(conn, &net.UDPAddr{}, request)
			}
			events := readEvents(t, f)
			if len(events) != 1 {
				t.Fatalf("events: %+v", events)
			}
			got, err := hex.DecodeString(events[0].RequestHex)
			if err != nil {
				t.Fatal(err)
			}
			// Option map iteration may change serialization order. Compare parsed objects.
			if protocol == 4 {
				original, err := dhcpv4.FromBytes(wire)
				if err != nil {
					t.Fatal(err)
				}
				recorded, err := dhcpv4.FromBytes(got)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(original, recorded) {
					t.Fatal("request evidence changed")
				}
			} else {
				original, err := dhcpv6.FromBytes(wire)
				if err != nil {
					t.Fatal(err)
				}
				recorded, err := dhcpv6.FromBytes(got)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(original, recorded) {
					t.Fatal("request evidence changed")
				}
			}
		}
	}
}
