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

package control

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestReplyValidate(t *testing.T) {
	tests := []struct {
		name      string
		protocol  int
		reply     Reply
		wantError bool
	}{
		{"valid hex", 4, Reply{Present: true, PayloadHex: "00aAFF"}, false},
		{"malformed URI bytes allowed", 6, Reply{Present: true, PayloadHex: "ff0001"}, false},
		{"invalid hex digit", 4, Reply{Present: true, PayloadHex: "gg"}, true},
		{"odd hex length", 6, Reply{Present: true, PayloadHex: "0"}, true},
		{"hex whitespace rejected", 4, Reply{Present: true, PayloadHex: "00 01"}, true},
		{"empty present option", 4, Reply{Present: true}, false},
		{"empty present IPv6 option", 6, Reply{Present: true}, false},
		{"missing option", 4, Reply{}, false},
		{"missing IPv6 option", 6, Reply{}, false},
		{"missing option with payload", 4, Reply{PayloadHex: "00"}, true},
		{"missing option with fragments", 4, Reply{Fragments: []int{1}}, true},
		{"silent", 4, Reply{Silent: true}, false},
		{"silent IPv6", 6, Reply{Silent: true}, false},
		{"silent present", 4, Reply{Silent: true, Present: true}, true},
		{"silent payload", 6, Reply{Silent: true, PayloadHex: "00"}, true},
		{"silent fragments", 4, Reply{Silent: true, Fragments: []int{1}}, true},
		{"minimum fragments", 4, Reply{Present: true, PayloadHex: "0001", Fragments: []int{1, 1}}, false},
		{"maximum fragment", 4, Reply{Present: true, PayloadHex: strings.Repeat("00", 255), Fragments: []int{255}}, false},
		{"multiple maximum fragments", 4, Reply{Present: true, PayloadHex: strings.Repeat("00", 510), Fragments: []int{255, 255}}, false},
		{"zero fragment", 4, Reply{Present: true, PayloadHex: "00", Fragments: []int{0, 1}}, true},
		{"negative fragment", 4, Reply{Present: true, PayloadHex: "00", Fragments: []int{-1, 2}}, true},
		{"oversized fragment", 4, Reply{Present: true, PayloadHex: strings.Repeat("00", 256), Fragments: []int{256}}, true},
		{"fragment total too short", 4, Reply{Present: true, PayloadHex: "0001", Fragments: []int{1}}, true},
		{"fragment total too long", 4, Reply{Present: true, PayloadHex: "00", Fragments: []int{2}}, true},
		{"empty option with fragment", 4, Reply{Present: true, Fragments: []int{1}}, true},
		{"IPv6 fragments", 6, Reply{Present: true, PayloadHex: "00", Fragments: []int{1}}, true},
		{"unsupported protocol", 5, Reply{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.reply.Validate(tt.protocol); (err != nil) != tt.wantError {
				t.Fatalf("Validate(%d) error = %v, wantError %v", tt.protocol, err, tt.wantError)
			}
		})
	}
}

func TestScenarioValidate(t *testing.T) {
	tests := []struct {
		name      string
		scenario  Scenario
		wantError string
	}{
		{"both sequences missing", Scenario{}, "v4"},
		{"IPv4 missing", Scenario{V6: []Reply{{}}}, "v4"},
		{"IPv6 missing", Scenario{V4: []Reply{{}}}, "v6"},
		{"empty sequence", Scenario{V4: []Reply{}, V6: []Reply{{}}}, "v4"},
		{"valid mixed replies", Scenario{V4: []Reply{{}, {Silent: true}, {Present: true}}, V6: []Reply{{Present: true}}}, ""},
		{"later IPv4 reply invalid", Scenario{V4: []Reply{{}, {Present: true, PayloadHex: "gg"}}, V6: []Reply{{}}}, "v4 reply 1"},
		{"later IPv6 reply invalid", Scenario{V4: []Reply{{}}, V6: []Reply{{}, {Silent: true, Present: true}}}, "v6 reply 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.scenario.Validate()
			if tt.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.wantError)
			}
		})
	}
}

func TestPayloadBytes(t *testing.T) {
	got, err := (Reply{Present: true, PayloadHex: "00aAFF"}).PayloadBytes()
	if err != nil || !bytes.Equal(got, []byte{0, 170, 255}) {
		t.Fatalf("PayloadBytes() = %x, %v", got, err)
	}
	if _, err := (Reply{PayloadHex: "gg"}).PayloadBytes(); err == nil {
		t.Fatal("invalid hex accepted")
	}
}

func TestJSONNames(t *testing.T) {
	scenario := Scenario{V4: []Reply{{Present: true, PayloadHex: "00", Fragments: []int{1}}}, V6: []Reply{{Silent: true}}}
	data, err := json.Marshal(scenario)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"v4":[{"present":true,"payload_hex":"00","fragments":[1],"silent":false}],"v6":[{"present":false,"payload_hex":"","fragments":null,"silent":true}]}`
	if !sameJSON(t, data, want) {
		t.Fatalf("JSON = %s, want %s", data, want)
	}
	event := Event{Protocol: 4, TransactionID: "abcd", Requested: true, ResponseHex: "00", Error: "example", At: time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)}
	data, err = json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	const wantEvent = `{"protocol":4,"transaction_id":"abcd","requested":true,"request_hex":"","response_hex":"00","error":"example","at":"2026-10-04T00:00:00Z"}`
	if !sameJSON(t, data, wantEvent) {
		t.Fatalf("event JSON = %s, want %s", data, wantEvent)
	}
	data, err = json.Marshal(Event{})
	if err != nil {
		t.Fatal(err)
	}
	if !sameJSON(t, data, `{"protocol":0,"transaction_id":"","requested":false,"request_hex":"","response_hex":"","error":"","at":"0001-01-01T00:00:00Z"}`) {
		t.Fatalf("empty event JSON = %s", data)
	}
	var decoded Scenario
	if err := json.Unmarshal([]byte(want), &decoded); err != nil {
		t.Fatal(err)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatal(err)
	}
}

func sameJSON(t *testing.T, got []byte, want string) bool {
	t.Helper()
	var actual, expected any
	if err := json.Unmarshal(got, &actual); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(actual, expected)
}
