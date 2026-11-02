//go:build linux && dhcp_integration

// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"encoding/hex"
	"flag"
	"fmt"
	"strings"
	"time"

	"fl-sztp-client/docker-tests/integration/internal/control"
)

const canonicalOrigin = "https://bootstrap:8080"
const sentinelOrigin = "https://fixture:9090"

type addressCase struct {
	name    string
	data    string
	present bool
	want    string
}

type integrationCase struct {
	discoverOnly    *discoveryReportCase
	scenario        control.Scenario
	want            string
	order           []int
	timeout         time.Duration
	silentProtocol  int
	timeoutObserved time.Time
	minuteRetry     bool
}

type discoveryReportCase struct {
	mode           string
	stopOnSuccess  bool
	skipV4, skipV6 string
	v6, v4         string
	configured     bool
	missingSerial  bool
	autoSerial     bool
	certificates   bool
	exitCode       int
}

func caseName(protocol int, name string) string { return fmt.Sprintf("v%d_%s", protocol, name) }

func rawReply(present bool, data string) control.Reply {
	return control.Reply{Present: present, PayloadHex: hex.EncodeToString([]byte(data))}
}

func dataScenario(protocol int, data addressCase) integrationCase {
	c := integrationCase{want: data.want, order: []int{4}, timeout: 20 * time.Second}
	if protocol == 4 {
		c.scenario = control.Scenario{V4: []control.Reply{rawReply(data.present, data.data)}, V6: []control.Reply{rawReply(true, sentinelOrigin)}}
		if data.want == "" {
			c.want, c.order = sentinelOrigin, []int{4, 6}
		}
	} else {
		c.scenario = control.Scenario{V4: []control.Reply{{}}, V6: []control.Reply{rawReply(data.present, data.data)}}
		c.order = []int{4, 6}
	}
	return c
}

func longURIOption() string {
	return "\x01\x2c" + canonicalOrigin + "/" + strings.Repeat("a", 277)
}

// Prefixes are literal RFC wire bytes, independent of the client's decoder and
// fixture packet builder. Their lengths count URI bytes, not characters.
func addressCases() []addressCase {
	single := "\x00\x16" + canonicalOrigin
	invalidHostname := "\x00\x1ehttps://bootstrap..example:443"
	return []addressCase{
		{"text_hostname", "bootstrap", true, "https://bootstrap"},
		{"text_hostname_port", "bootstrap:8080", true, canonicalOrigin},
		{"text_restconf", canonicalOrigin + "/restconf/operations/ietf-sztp-bootstrap-server:get-bootstrapping-data", true, canonicalOrigin},
		{"text_http", "http://bootstrap:8080", true, canonicalOrigin},
		{"text_userinfo_path_query_fragment", "https://user:secret@bootstrap:8080/path?x=1#f", true, canonicalOrigin},
		{"text_bracketed_ipv6", "[2001:db8::1]:8080", true, "https://[2001:db8::1]:8080"},
		{"rfc_single", single, true, canonicalOrigin},
		{"rfc_multiple_first_wins", single + "\x00\x12https://other:8081", true, canonicalOrigin},
		{"rfc_bad_before_valid", "\x00\x08https://" + single, true, canonicalOrigin},
		{"rfc_invalid_hostname_before_valid", invalidHostname + single, true, canonicalOrigin},
		{"rfc_empty_before_valid", "\x00\x00" + single, true, canonicalOrigin},
		{"rfc_unsupported_before_valid", "\x00\x14ftp://bootstrap:8080" + single, true, canonicalOrigin},
		{"rfc_invalid_utf8_before_valid", "\x00\x02\xff\xff" + single, true, canonicalOrigin},
		{"rfc_valid_before_bad_content", single + "\x00\x08https://", true, canonicalOrigin},
		{"rfc_path_query_fragment", "\x00\x21" + canonicalOrigin + "/path?x=1#f", true, canonicalOrigin},
		{"rfc_ipv6", "\x00\x1ahttps://[2001:db8::1]:8080", true, "https://[2001:db8::1]:8080"},
		{"rfc_unicode_byte_length", "\x00\x19" + canonicalOrigin + "/ä", true, canonicalOrigin},
		{"rfc_254_bytes", "\x00\xfe" + canonicalOrigin + "/" + strings.Repeat("a", 231), true, canonicalOrigin},
		{"rfc_300_bytes", longURIOption(), true, canonicalOrigin},
		{"rfc_http", "\x00\x15http://bootstrap:8080", true, canonicalOrigin},
		{"rfc_bare_authority", "\x00\x0ebootstrap:8080", true, canonicalOrigin},
		{"invalid_text_hostname", "https://bootstrap..example:443", true, ""},
		{"invalid_rfc_hostname", invalidHostname, true, ""},
		{"invalid_truncated_prefix", "\x00", true, ""},
		{"invalid_truncated_payload", "\x00\x16https://bootstrap", true, ""},
		{"invalid_later_prefix_after_valid", single + "\x00", true, ""},
		{"invalid_later_payload_after_valid", single + "\x00\x05abc", true, ""},
		{"invalid_trailing_byte", single + "\xff", true, ""},
		{"invalid_one_byte_length", "\x16" + canonicalOrigin, true, ""},
		{"invalid_little_endian_length", "\x16\x00" + canonicalOrigin, true, ""},
		{"invalid_utf8_text", "\xff", true, ""},
		{"invalid_maximum_length", "\xff\xff", true, ""},
		{"invalid_no_valid_uri", "\x00\x08https://\x00\x14ftp://bootstrap:8080", true, ""},
		{"invalid_empty_uri", "\x00\x00", true, ""},
		{"invalid_empty_option", "", true, ""},
		{"absent_option", "", false, ""},
	}
}

// namedCase is the sole catalog entry used by execution and selection.
type namedCase struct {
	name string
	integrationCase
	slow bool
}

var selectedCase = flag.String("dhcp.case", "", "exact DHCP catalog case name")

func caseCatalog() []namedCase {
	var cases []namedCase
	for _, protocol := range []int{4, 6} {
		for _, data := range addressCases() {
			cases = append(cases, namedCase{name: caseName(protocol, data.name), integrationCase: dataScenario(protocol, data)})
		}
	}
	fragmented := dataScenario(4, addressCase{present: true, data: longURIOption(), want: canonicalOrigin})
	fragmented.scenario.V4[0].Fragments = []int{1, 255, 46}
	cases = append(cases, namedCase{name: "v4_fragmented_length_and_payload", integrationCase: fragmented})
	cases = append(cases,
		namedCase{name: "silent_v4_timeout_then_v6", slow: true, integrationCase: integrationCase{
			scenario: control.Scenario{V4: []control.Reply{{Silent: true}}, V6: []control.Reply{rawReply(true, sentinelOrigin)}},
			want:     sentinelOrigin, order: []int{4, 6}, timeout: 55 * time.Second, silentProtocol: 4,
		}},
		namedCase{name: "real_minute_retry_then_v6", slow: true, integrationCase: integrationCase{
			scenario: control.Scenario{V6: []control.Reply{{}, rawReply(true, "\x00\x16"+canonicalOrigin)}, V4: []control.Reply{{}}},
			want:     canonicalOrigin, order: []int{4, 6, 4, 6}, timeout: 90 * time.Second, minuteRetry: true,
		}},
		namedCase{name: "silent_v6_timeout_then_retry", slow: true, integrationCase: integrationCase{
			scenario: control.Scenario{V4: []control.Reply{{}}, V6: []control.Reply{{Silent: true}}},
			order:    []int{4, 6}, timeout: 55 * time.Second, silentProtocol: 6,
		}},
	)
	return append(cases, discoveryReportCases()...)
}

func discoveryReportCases() []namedCase {
	valid6 := rawReply(true, canonicalOrigin)
	valid4 := rawReply(true, sentinelOrigin)
	invalid := rawReply(true, "https://bad..example")
	var cases []namedCase
	for _, tc := range []struct {
		name   string
		v6, v4 control.Reply
		report discoveryReportCase
		silent bool
		order  []int
	}{
		{"both_valid", valid6, valid4, discoveryReportCase{v6: canonicalOrigin, v4: sentinelOrigin}, false, nil},
		{"v4_only", invalid, valid4, discoveryReportCase{v4: sentinelOrigin}, false, nil},
		{"v6_only", valid6, control.Reply{}, discoveryReportCase{v6: canonicalOrigin}, false, nil},
		{"invalid_both", invalid, invalid, discoveryReportCase{exitCode: 1}, false, nil},
		{"configured_server", valid6, valid4, discoveryReportCase{v6: canonicalOrigin, v4: sentinelOrigin, configured: true}, false, nil},
		{"configured_server_without_dhcp", control.Reply{}, control.Reply{}, discoveryReportCase{configured: true, exitCode: 1}, false, nil},
		{"missing_serial", valid6, valid4, discoveryReportCase{v6: canonicalOrigin, v4: sentinelOrigin, missingSerial: true, exitCode: 1}, false, nil},
		{"hardware_serial_and_certificates", valid6, valid4, discoveryReportCase{v6: canonicalOrigin, v4: sentinelOrigin, autoSerial: true, certificates: true}, false, nil},
		{"silent_v6", control.Reply{Silent: true}, valid4, discoveryReportCase{v4: sentinelOrigin}, true, nil},
		{"dhcp4_selected", control.Reply{Silent: true}, valid4, discoveryReportCase{mode: "--discover-only-dhcp4", v4: sentinelOrigin, skipV6: "not selected"}, false, []int{4}},
		{"dhcp6_selected", valid6, control.Reply{Silent: true}, discoveryReportCase{mode: "--discover-only-dhcp6", v6: canonicalOrigin, skipV4: "not selected"}, false, []int{6}},
		{"stop_after_v4_success", control.Reply{Silent: true}, valid4, discoveryReportCase{stopOnSuccess: true, v4: sentinelOrigin, skipV6: "bootstrap server already found via DHCPv4"}, false, []int{4}},
		{"stop_continues_after_invalid_v4", valid6, invalid, discoveryReportCase{stopOnSuccess: true, v6: canonicalOrigin}, false, []int{4, 6}},
	} {
		report := tc.report
		c := integrationCase{discoverOnly: &report, scenario: control.Scenario{V6: []control.Reply{tc.v6}, V4: []control.Reply{tc.v4}}, order: []int{4, 6}, timeout: 20 * time.Second}
		if tc.order != nil {
			c.order = tc.order
		}
		if tc.silent {
			c.silentProtocol, c.timeout = 6, 55*time.Second
		}
		cases = append(cases, namedCase{name: "discover_only_" + tc.name, integrationCase: c, slow: tc.silent})
	}
	return cases
}

func selectCases(name string, quick bool) ([]namedCase, error) {
	catalog := caseCatalog()
	var chosen []namedCase
	var valid []string
	for _, c := range catalog {
		valid = append(valid, c.name)
		if name != "" && name != c.name {
			continue
		}
		if quick && c.slow {
			if name != "" {
				return nil, fmt.Errorf("case %q requires real timeout/retry checks and cannot run with --quick", name)
			}
			continue
		}
		chosen = append(chosen, c)
	}
	if len(chosen) == 0 {
		return nil, fmt.Errorf("unknown DHCP case %q; valid cases: %s", name, strings.Join(valid, ", "))
	}
	return chosen, nil
}
