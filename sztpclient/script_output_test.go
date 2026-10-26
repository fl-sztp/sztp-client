//go:build linux || darwin

// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package sztpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"fl-sztp-client/internal/logging"
)

func TestScriptLogWriterPreservesChunksAndFinalLine(t *testing.T) {
	for _, stream := range []string{"stdout", "stderr"} {
		t.Run(stream, func(t *testing.T) {
			var output bytes.Buffer
			writer := newScriptLogWriter(slog.New(slog.NewJSONHandler(&output, nil)), "post", stream)
			input := "first\n" + strings.Repeat("x", 4097) + "\nlast"
			for _, chunk := range []string{input[:3], input[3:10], input[10:]} {
				if n, err := writer.Write([]byte(chunk)); n != len(chunk) || err != nil {
					t.Fatalf("write=%d, %v", n, err)
				}
			}
			writer.Flush()
			writer.Flush()
			var messages []string
			decoder := json.NewDecoder(&output)
			for decoder.More() {
				var record map[string]any
				if err := decoder.Decode(&record); err != nil {
					t.Fatal(err)
				}
				if record["level"] != "INFO" || record["phase"] != "post" || record["stream"] != stream {
					t.Fatalf("lost script context: %v", record)
				}
				messages = append(messages, record["msg"].(string))
			}
			want := []string{"first", strings.Repeat("x", 4096), "x", "last"}
			if !reflect.DeepEqual(messages, want) {
				var lengths []int
				for _, message := range messages {
					lengths = append(lengths, len(message))
				}
				t.Fatalf("incorrect output chunks: lengths=%v", lengths)
			}
		})
	}
}

func TestScriptLogsBothStreamsAndBoundsServerOutput(t *testing.T) {
	pre := "printf 'stdout-start\\n'; printf '%070000d' 0; printf '\\nstdout-end'; printf 'stderr-sentinel' >&2\n"
	client, logs, reports := onboardingTestClient(t, t.TempDir(), onboardingTestFields("", pre, ""), nil)
	client.Logger = logging.New(logs, false)
	if _, err := client.RunBootstrapSequence(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"stdout-start", "stdout-end", "stderr-sentinel", "phase=pre", "stream=stdout", "stream=stderr"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("normal-level logs lack %q", want)
		}
	}
	found := false
	for len(reports) > 0 {
		input := (<-reports).IetfSztpBootstrapServerInput
		if input.ProgressType != "informational" || !strings.Contains(input.Message, "stdout-end") {
			continue
		}
		found = true
		if !strings.Contains(input.Message, "[truncated; last 64 KiB of stdout]") ||
			strings.Contains(input.Message, "stdout-start") || strings.Contains(input.Message, "stderr-sentinel") ||
			len(input.Message) > scriptOutputLimit+100 {
			t.Fatalf("stdout report was not correctly bounded: length=%d", len(input.Message))
		}
	}
	if !found {
		t.Fatal("missing informational stdout report")
	}
}

func TestScriptOutputTailHandlesLargeAndSmallWrites(t *testing.T) {
	for _, chunks := range [][]string{
		{strings.Repeat("x", 100000), "end"},
		{strings.Repeat("a", 32000), strings.Repeat("b", 32000), strings.Repeat("c", 32000)},
		{"small", " output"},
	} {
		var tail scriptOutputTail
		all := strings.Join(chunks, "")
		for _, chunk := range chunks {
			if n, err := tail.Write([]byte(chunk)); n != len(chunk) || err != nil {
				t.Fatalf("write=%d, %v", n, err)
			}
			if len(tail.data) > scriptOutputLimit {
				t.Fatal("stdout buffer exceeded limit")
			}
		}
		want := all[max(0, len(all)-scriptOutputLimit):]
		if string(tail.data) != want || tail.truncated != (len(all) > scriptOutputLimit) {
			t.Fatal("incorrect stdout tail")
		}
	}
}
