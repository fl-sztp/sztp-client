//go:build linux || darwin

// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fl-sztp-client/sztpclient"
)

func TestWorkspaceCleanupAfterLocalSuccessAndFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			config := testBootstrapConfig(t)
			if err := os.Mkdir(config.TmpFolder, 0700); err != nil {
				t.Fatal(err)
			}
			external := filepath.Join(config.TmpFolder, "keep.log")
			if err := os.WriteFile(external, []byte("external log"), 0600); err != nil {
				t.Fatal(err)
			}
			var workspace string
			err := runTestOnboarding(tmpTestLogger(new(bytes.Buffer)), config, func() (string, error) {
				workspace = config.TmpFolder
				data, err := os.ReadFile(filepath.Join(config.StateDir, workspaceStateFile))
				var registered workspaceRecord
				if err != nil || json.Unmarshal(data, &registered) != nil || registered.Path != workspace {
					t.Fatal("workspace was not registered before bootstrap")
				}
				file, err := os.Create(filepath.Join(workspace, "large-download"))
				if err != nil {
					t.Fatal(err)
				}
				// Sparse file: exercise large downloads without allocating 100 GiB.
				if err := file.Truncate(100 << 30); err != nil {
					file.Close()
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(workspace, "script.log"), []byte("temporary"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(external, filepath.Join(workspace, "external-link")); err != nil {
					t.Fatal(err)
				}
				if fail {
					return "", errors.New("bootstrap failed")
				}
				return "serial", nil
			})
			if (err != nil) != fail {
				t.Fatalf("run error=%v", err)
			}
			assertWorkspaceRemoved(t, config.StateDir, workspace)
			data, err := os.ReadFile(external)
			if err != nil || string(data) != "external log" {
				t.Fatalf("external file changed: %q (%v)", data, err)
			}
		})
	}
}

func assertWorkspaceRemoved(t *testing.T, stateDir, workspace string) {
	t.Helper()
	for _, path := range []string{workspace, filepath.Join(stateDir, workspaceStateFile)} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("workspace artifact remains at %s: %v", path, err)
		}
	}
}

func TestWorkspaceRecoveryOnCompletedStartAndReset(t *testing.T) {
	for _, reset := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed start", true: "reset"}[reset], func(t *testing.T) {
			config := testBootstrapConfig(t)
			logger := tmpTestLogger(new(bytes.Buffer))
			store, err := openOnboardingStore(config.StateDir)
			if err != nil {
				t.Fatal(err)
			}
			workspace, err := prepareTmpFolder(logger, config.TmpFolder)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.RegisterWorkspace(workspace); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(workspace, "left-after-crash"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			if err := store.Save(legacyState("serial")); err != nil {
				t.Fatal(err)
			}
			store.Close()
			config.ResetOnboarding = reset
			if err := runTestOnboarding(logger, config, func() (string, error) {
				t.Fatal("recovery reran provisioning")
				return "", nil
			}); err != nil {
				t.Fatal(err)
			}
			assertWorkspaceRemoved(t, config.StateDir, workspace)
		})
	}
}

func TestWorkspaceCleanupFailureBlocksProvisioning(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	config := testBootstrapConfig(t)
	logger := tmpTestLogger(new(bytes.Buffer))
	store, err := openOnboardingStore(config.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := prepareTmpFolder(logger, config.TmpFolder)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterWorkspace(workspace); err != nil {
		t.Fatal(err)
	}
	store.Close()
	blocked := filepath.Join(workspace, "blocked")
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "keep"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(blocked, 0700)
	for range 2 {
		err := runTestOnboarding(logger, config, func() (string, error) {
			t.Fatal("failed cleanup allowed new bootstrap")
			return "", nil
		})
		if err == nil || !strings.Contains(err.Error(), "remove workspace") {
			t.Fatalf("cleanup error=%v", err)
		}
		if _, err := os.Stat(filepath.Join(config.StateDir, workspaceStateFile)); err != nil {
			t.Fatalf("failed cleanup discarded registration: %v", err)
		}
	}
	if err := os.Chmod(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	config.ResetOnboarding = true
	if err := runTestOnboarding(logger, config, nil); err != nil {
		t.Fatal(err)
	}
	assertWorkspaceRemoved(t, config.StateDir, workspace)
}

func TestWorkspaceRegistrationRejectsUnownedPaths(t *testing.T) {
	store := testOnboardingStore(t)
	root := t.TempDir()
	for _, path := range []string{"/", root, "relative/sztp-client-20261020T100000Z-123", filepath.Join(root, "sztp-client-other")} {
		if err := store.writeJSON(workspaceStateFile, workspaceRecord{Path: path}); err != nil {
			t.Fatal(err)
		}
		if err := store.CleanupWorkspace(); err == nil {
			t.Fatalf("unsafe workspace accepted: %s", path)
		}
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("unrelated folder removed: %v", err)
	}
}

func TestWorkspaceCleanupPreservesStateAndSymlinkTargets(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "sztp-client-20261020T100000Z-123")
	store, err := openOnboardingStore(filepath.Join(workspace, "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.writeJSON(workspaceStateFile, workspaceRecord{Path: workspace}); err != nil {
		t.Fatal(err)
	}
	if err := store.CleanupWorkspace(); err == nil {
		t.Fatal("cleanup accepted a workspace containing persistent state")
	}
	if _, err := os.Stat(filepath.Join(store.dir, ".onboarding.lock")); err != nil {
		t.Fatal(err)
	}

	other := testOnboardingStore(t)
	link := filepath.Join(root, "sztp-client-20261020T100000Z-456")
	if err := os.Symlink(workspace, link); err != nil {
		t.Fatal(err)
	}
	if err := other.RegisterWorkspace(link); err != nil {
		t.Fatal(err)
	}
	if err := other.CleanupWorkspace(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.dir); err != nil {
		t.Fatalf("cleanup followed workspace symlink: %v", err)
	}
}

func TestBootstrapRetryCleansFailedArtifacts(t *testing.T) {
	for _, failure := range []string{"preparation", "pre", "post"} {
		t.Run(failure, func(t *testing.T) {
			config := testBootstrapConfig(t)
			config.AllowInsecure = true
			logger := tmpTestLogger(new(bytes.Buffer))
			var attempts atomic.Int32
			waits := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/.well-known/host-meta":
					_, _ = w.Write([]byte(`<XRD xmlns="http://docs.oasis-open.org/ns/xri/xrd-1.0"><Link rel="restconf" href="/restconf"/></XRD>`))
				case "/restconf/operations/ietf-sztp-bootstrap-server:get-bootstrapping-data":
					attempt := attempts.Add(1)
					fields := map[string]string{}
					if attempt == 1 {
						if failure == "preparation" {
							fields["configuration"] = "%%%"
						} else {
							fields[failure+"-configuration-script"] = base64.StdEncoding.EncodeToString([]byte("echo leftover > ./download; exit 1\n"))
						}
					}
					info, _ := json.Marshal(map[string]any{"ietf-sztp-conveyed-info:onboarding-information": fields})
					_ = json.NewEncoder(w).Encode(conveyedResponseFixture(t, info))
				default:
					w.WriteHeader(http.StatusNoContent)
				}
			}))
			defer server.Close()
			config.Server = server.URL
			var workspace string
			err := runOnboarding(context.Background(), logger, config, func(ctx context.Context) (sztpclient.CompletionReport, error) {
				workspace = config.TmpFolder
				return runBootstrapWithRetry(ctx, logger, config, func(_ context.Context, delay time.Duration) error {
					waits++
					if delay != time.Minute {
						t.Errorf("bootstrap retry delay=%s", delay)
					}
					entries, err := os.ReadDir(workspace)
					if err != nil || len(entries) != 0 {
						t.Fatalf("failed attempt retained files: %v (%v)", entries, err)
					}
					return nil
				})
			}, func(context.Context, sztpclient.CompletionReport) error { return nil })
			if err != nil || attempts.Load() != 2 || waits != 1 {
				t.Fatalf("attempts=%d waits=%d error=%v", attempts.Load(), waits, err)
			}
			assertWorkspaceRemoved(t, config.StateDir, workspace)
		})
	}
}
