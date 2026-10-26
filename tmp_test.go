// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"fl-sztp-client/internal/logging"
)

func tmpTestLogger(output *bytes.Buffer) *slog.Logger {
	return logging.New(output, false)
}

func assertAutomaticTmpFolder(t *testing.T, path, parent string) {
	t.Helper()
	parent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != parent {
		t.Fatalf("temporary folder = %q, want parent %q", path, parent)
	}
	name := strings.TrimPrefix(filepath.Base(path), "sztp-client-")
	timestamp, suffix, ok := strings.Cut(name, "-")
	if !ok || suffix == "" {
		t.Fatalf("temporary folder %q lacks timestamp or random suffix", path)
	}
	created, err := time.Parse("20060102T150405Z", timestamp)
	if err != nil || created.Location() != time.UTC {
		t.Fatalf("timestamp %q is not valid UTC: %v", timestamp, err)
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		t.Fatalf("temporary folder %q is not a directory: %v", path, err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		t.Errorf("automatic folder permissions = %o, want no group or other access", info.Mode().Perm())
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary folder retains probe files: %v (%v)", entries, err)
	}
}

func TestPrepareTmpFolderAutomatic(t *testing.T) {
	parent := t.TempDir()
	t.Setenv("TMPDIR", parent)
	var output bytes.Buffer
	logger := tmpTestLogger(&output)
	first, err := prepareTmpFolder(logger, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := prepareTmpFolder(logger, "")
	if err != nil {
		t.Fatal(err)
	}
	assertAutomaticTmpFolder(t, first, parent)
	assertAutomaticTmpFolder(t, second, parent)
	if first == second {
		t.Fatal("separate runs selected the same automatic directory")
	}
	if output.Len() != 0 {
		t.Fatalf("automatic selection logged a warning: %s", &output)
	}
}

func TestPrepareTmpFolderConfigured(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "create missing folder"
		if existing {
			name = "reuse existing folder"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			parent := filepath.Join(root, "automatic")
			if err := os.Mkdir(parent, 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("TMPDIR", parent)
			requested := filepath.Join(root, "requested folder")
			if existing {
				if err := os.Mkdir(requested, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(requested, "configuration"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var output bytes.Buffer
			got, err := prepareTmpFolder(tmpTestLogger(&output), requested)
			if err != nil {
				t.Fatalf("prepareTmpFolder() = %q, %v; want %q", got, err, requested)
			}
			assertAutomaticTmpFolder(t, got, requested)
			entries, err := os.ReadDir(requested)
			wantEntries := 1
			if existing {
				wantEntries = 2
				data, err := os.ReadFile(filepath.Join(requested, "configuration"))
				if err != nil || string(data) != "keep" {
					t.Fatalf("existing artifact changed: %q (%v)", data, err)
				}
				info, err := os.Stat(requested)
				if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0700) {
					t.Fatalf("existing folder permissions changed: %v (%v)", info, err)
				}
			}
			if err != nil || len(entries) != wantEntries {
				t.Fatalf("unexpected files in configured folder: %v (%v)", entries, err)
			}
			automaticEntries, err := os.ReadDir(parent)
			if err != nil || len(automaticEntries) != 0 || output.Len() != 0 {
				t.Fatalf("working configured folder triggered fallback: %v (%v), log: %s", automaticEntries, err, &output)
			}
		})
	}
}

func TestPrepareTmpFolderFallback(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		setup        func(*testing.T, string) string
	}{
		{
			name: "path is a file", reason: "not a directory",
			setup: func(t *testing.T, root string) string {
				path := filepath.Join(root, "requested")
				if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
				return path
			},
		},
		{
			name: "folder creation fails", reason: "create temporary folder",
			setup: func(t *testing.T, root string) string {
				return filepath.Join(root, "missing-parent", "requested")
			},
		},
		{
			name: "folder is not writable", reason: "create temporary write probe",
			setup: func(t *testing.T, root string) string {
				if runtime.GOOS == "windows" {
					t.Skip("requires Unix directory permissions")
				}
				path := filepath.Join(root, "readonly")
				if err := os.Mkdir(path, 0500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := os.Chmod(path, 0700); err != nil {
						t.Error(err)
					}
				})
				file, err := os.CreateTemp(path, "permission-check-")
				if err == nil {
					file.Close()
					os.Remove(file.Name())
					t.Skip("process can write to a read-only directory (e.g. root)")
				}
				if !os.IsPermission(err) {
					t.Fatalf("read-only fixture failed unexpectedly: %v", err)
				}
				return path
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, parent := t.TempDir(), t.TempDir()
			t.Setenv("TMPDIR", parent)
			requested := tc.setup(t, root)
			var output bytes.Buffer
			got, err := prepareTmpFolder(tmpTestLogger(&output), requested)
			if err != nil {
				t.Fatal(err)
			}
			assertAutomaticTmpFolder(t, got, parent)
			for _, want := range []string{"level=WARN", requested, tc.reason, "falling back"} {
				if !strings.Contains(output.String(), want) {
					t.Errorf("fallback log lacks %q: %s", want, &output)
				}
			}
			matches, err := filepath.Glob(filepath.Join(requested, ".sztp-client-probe-*"))
			if err != nil || len(matches) != 0 {
				t.Fatalf("probe files remain in failed configured folder: %v (%v)", matches, err)
			}
		})
	}
}

func TestPrepareTmpFolderDefaultFailure(t *testing.T) {
	for _, configured := range []bool{false, true} {
		name := "automatic default fails"
		if configured {
			name = "configured folder and fallback both fail"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			parent := filepath.Join(root, "missing-default-parent")
			t.Setenv("TMPDIR", parent)
			requested := ""
			if configured {
				requested = filepath.Join(root, "requested-file")
				if err := os.WriteFile(requested, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var output bytes.Buffer
			got, err := prepareTmpFolder(tmpTestLogger(&output), requested)
			if got != "" || err == nil {
				t.Fatalf("prepareTmpFolder() = %q, %v; want empty path and error", got, err)
			}
			for _, want := range []string{"default temporary folder", parent} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error lacks %q: %v", want, err)
				}
			}
			if configured {
				for _, want := range []string{"configured temporary folder", requested, "not a directory"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error lost configured folder failure %q: %v", want, err)
					}
				}
			} else if output.Len() != 0 {
				t.Fatalf("empty tmp logged a configured folder warning: %s", &output)
			}
		})
	}
}
