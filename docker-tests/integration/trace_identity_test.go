//go:build linux && dhcp_integration

// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"encoding/hex"
	"fl-sztp-client/docker-tests/integration/internal/control"
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/iana"
	"net"
	"testing"
)

func TestRequestEvidenceAndIdentity(t *testing.T) {
	mac := net.HardwareAddr{2, 0, 0, 0, 0, 7}
	request4, err := dhcpv4.NewDiscovery(mac, dhcpv4.WithRequestedOptions(dhcpv4.GenericOptionCode(143)), dhcpv4.WithOption(dhcpv4.OptGeneric(dhcpv4.OptionClientIdentifier, []byte{9, 8, 7})))
	if err != nil {
		t.Fatal(err)
	}
	event4 := control.Event{Protocol: 4, TransactionID: hex.EncodeToString(request4.TransactionID[:]), RequestHex: hex.EncodeToString(request4.ToBytes())}
	parsed4, _, err := decodeRequest(event4)
	if err != nil {
		t.Fatal(err)
	}
	response4, err := dhcpv4.NewReplyFromRequest(parsed4, dhcpv4.WithMessageType(dhcpv4.MessageTypeOffer))
	if err != nil {
		t.Fatal(err)
	}
	if err := identity4(parsed4, response4); err != nil {
		t.Fatal(err)
	}
	response4.UpdateOption(dhcpv4.OptMessageType(dhcpv4.MessageTypeAck))
	if identity4(parsed4, response4) == nil {
		t.Fatal("wrong IPv4 response type accepted")
	}
	response4.UpdateOption(dhcpv4.OptMessageType(dhcpv4.MessageTypeOffer))
	response4.TransactionID[0] ^= 1
	if identity4(parsed4, response4) == nil {
		t.Fatal("changed IPv4 transaction accepted")
	}
	response4.TransactionID = parsed4.TransactionID
	response4.UpdateOption(dhcpv4.OptGeneric(dhcpv4.OptionClientIdentifier, []byte{1}))
	if identity4(parsed4, response4) == nil {
		t.Fatal("changed IPv4 client accepted")
	}
	for _, duid := range []dhcpv6.DUID{&dhcpv6.DUIDLL{HWType: iana.HWTypeEthernet, LinkLayerAddr: mac}, &dhcpv6.DUIDLLT{HWType: iana.HWTypeEthernet, LinkLayerAddr: mac, Time: 123}} {
		request, err := dhcpv6.NewSolicit(mac, dhcpv6.WithRequestedOptions(dhcpv6.OptionCode(136)), dhcpv6.WithClientID(duid))
		if err != nil {
			t.Fatal(err)
		}
		event := control.Event{Protocol: 6, TransactionID: hex.EncodeToString(request.TransactionID[:]), RequestHex: hex.EncodeToString(request.ToBytes())}
		_, parsed, err := decodeRequest(event)
		if err != nil {
			t.Fatal(err)
		}
		response, err := dhcpv6.NewAdvertiseFromSolicit(parsed, dhcpv6.WithServerID(duid))
		if err != nil {
			t.Fatal(err)
		}
		if err := identity6(parsed, response); err != nil {
			t.Fatal(err)
		}
		response.MessageType = dhcpv6.MessageTypeReply
		if identity6(parsed, response) == nil {
			t.Fatal("wrong IPv6 response type accepted")
		}
		response.MessageType = dhcpv6.MessageTypeAdvertise
		response.TransactionID[0] ^= 1
		if identity6(parsed, response) == nil {
			t.Fatal("changed IPv6 transaction accepted")
		}
		response.TransactionID = parsed.TransactionID
		response.Options.Del(dhcpv6.OptionClientID)
		response.AddOption(dhcpv6.OptClientID(&dhcpv6.DUIDLL{HWType: iana.HWTypeEthernet, LinkLayerAddr: net.HardwareAddr{2, 0, 0, 0, 0, 8}}))
		if identity6(parsed, response) == nil {
			t.Fatal("changed IPv6 client accepted")
		}
	}
	for _, protocol := range []int{4, 6} {
		for _, evidence := range []string{"", "gg", "00"} {
			if _, _, err := decodeRequest(control.Event{Protocol: protocol, RequestHex: evidence}); err == nil {
				t.Fatalf("accepted invalid request %q", evidence)
			}
		}
	}
	event4.TransactionID = "00000000"
	if _, _, err := decodeRequest(event4); err == nil {
		t.Fatal("mismatched event transaction accepted")
	}
	request4.DeleteOption(dhcpv4.OptionParameterRequestList)
	event4.TransactionID = hex.EncodeToString(request4.TransactionID[:])
	event4.RequestHex = hex.EncodeToString(request4.ToBytes())
	if _, _, err := decodeRequest(event4); err == nil {
		t.Fatal("missing requested option accepted")
	}
}
