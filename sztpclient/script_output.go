// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package sztpclient

import (
	"bytes"
	"log/slog"
)

const scriptOutputLimit = 64 * 1024

// Keep server reports bounded independently of the full output sent to the
// local logger. Only stdout is included in the informational server report.
type scriptOutputTail struct {
	data      []byte
	truncated bool
}

func (w *scriptOutputTail) Write(p []byte) (int, error) {
	n := len(p)
	if len(w.data)+n > scriptOutputLimit {
		w.truncated = true
		if n >= scriptOutputLimit {
			w.data = append(w.data[:0], p[n-scriptOutputLimit:]...)
			return n, nil
		}
		w.data = w.data[len(w.data)+n-scriptOutputLimit:]
	}
	w.data = append(w.data, p...)
	return n, nil
}

func (w *scriptOutputTail) String() string {
	if w.truncated {
		return "[truncated; last 64 KiB of stdout]\n" + string(w.data)
	}
	return string(w.data)
}

// Split long lines as well as newline-delimited output so scripts cannot cause
// unbounded buffering by writing a single very long line.
type scriptLogWriter struct {
	logger  *slog.Logger
	pending []byte
}

func newScriptLogWriter(logger *slog.Logger, phase, stream string) *scriptLogWriter {
	return &scriptLogWriter{logger: logger.With("phase", phase, "stream", stream)}
}

func (w *scriptLogWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		size := min(len(p), 4096-len(w.pending))
		newline := bytes.IndexByte(p[:size], '\n')
		if newline >= 0 {
			w.pending = append(w.pending, p[:newline]...)
			w.logger.Info(string(w.pending))
			w.pending = w.pending[:0]
			p = p[newline+1:]
			continue
		}
		w.pending = append(w.pending, p[:size]...)
		p = p[size:]
		if len(w.pending) == 4096 {
			w.Flush()
		}
	}
	return n, nil
}

func (w *scriptLogWriter) Flush() {
	if len(w.pending) > 0 {
		w.logger.Info(string(w.pending))
		w.pending = w.pending[:0]
	}
}
