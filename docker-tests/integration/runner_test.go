//go:build !dhcp_integration

// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Exercise the host runner with fake Docker orchestration. DHCP protocol behavior
// belongs to the real container suite; this checks invocation and exit handling.
func TestRunner(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		testerStatus string
		wantStatus   int
		wantArgs     []string
	}{
		{name: "full", testerStatus: "0", wantArgs: []string{"--rm", "--no-deps", "tester", "tester"}},
		{name: "full tester failure", testerStatus: "23", wantStatus: 23, wantArgs: []string{"--rm", "--no-deps", "tester", "tester"}},
		{name: "quick", args: []string{"--quick"}, testerStatus: "0", wantArgs: []string{"--rm", "--no-deps", "tester", "tester", "-test.short"}},
		{name: "exact boundaries", args: []string{"--case", "case with spaces"}, testerStatus: "0", wantArgs: []string{"--rm", "--no-deps", "tester", "tester", "-dhcp.case=case with spaces"}},
		{name: "quick exact", args: []string{"--quick", "--case", "one"}, testerStatus: "0", wantArgs: []string{"--rm", "--no-deps", "tester", "tester", "-test.short", "-dhcp.case=one"}},
		{name: "repeated quick", args: []string{"--quick", "--quick"}, wantStatus: 2},
		{name: "missing case", args: []string{"--case"}, wantStatus: 2},
		{name: "unknown flag", args: []string{"--unknown"}, wantStatus: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			fake := `#!/bin/sh
printf 'docker\n' >> "$RUNNER_CALLS"
if [ "$1" = compose ]; then
 shift 5
 command=$1
 shift
 case "$command" in
  run) printf '%s\n' "$@" > "$RUNNER_ARGS"; echo tester-output; exit "$RUNNER_TESTER_STATUS" ;;
  down) echo cleaned > "$RUNNER_CLEANUP" ;;
 esac
elif [ "$1" = info ]; then
 echo linux/x86_64
elif [ "$1" = image ]; then
 echo linux/amd64
elif [ "$1" = network ] && [ "$2" = inspect ]; then
 if [ "$3" = --format ]; then echo false/true/false; else echo '{}'; fi
fi
`
			if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(fake), 0755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("/bin/bash", append([]string{"run.sh"}, tt.args...)...)
			cmd.Env = append(os.Environ(), "SZTP_DHCP_PLATFORM=", "PATH="+dir+":"+os.Getenv("PATH"), "TMPDIR="+dir, "RUNNER_CALLS="+filepath.Join(dir, "calls"), "RUNNER_ARGS="+filepath.Join(dir, "args"), "RUNNER_CLEANUP="+filepath.Join(dir, "cleanup"), "RUNNER_TESTER_STATUS="+tt.testerStatus)
			output, err := cmd.CombinedOutput()
			status := 0
			if err != nil {
				if exit, ok := err.(*exec.ExitError); ok {
					status = exit.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if status != tt.wantStatus {
				t.Errorf("exit = %d, want %d; output:\n%s", status, tt.wantStatus, output)
			}
			args, readErr := os.ReadFile(filepath.Join(dir, "args"))
			if tt.wantArgs == nil {
				if !os.IsNotExist(readErr) {
					t.Errorf("invalid flags reached tester: %q (%v)", args, readErr)
				}
				if _, err := os.Stat(filepath.Join(dir, "calls")); !os.IsNotExist(err) {
					t.Errorf("invalid flags invoked Docker: %v", err)
				}
				return
			}
			if readErr != nil {
				t.Fatalf("tester was not invoked: %v; output:\n%s", readErr, output)
			}
			gotArgs := strings.Split(strings.TrimSuffix(string(args), "\n"), "\n")
			if !reflect.DeepEqual(gotArgs, tt.wantArgs) {
				t.Errorf("tester args = %q, want %q", gotArgs, tt.wantArgs)
			}
			if _, err := os.Stat(filepath.Join(dir, "cleanup")); err != nil {
				t.Errorf("project cleanup missing: %v", err)
			}
			artifacts, err := filepath.Glob(filepath.Join(dir, "sztp-dhcp.*"))
			if err != nil || len(artifacts) != 1 {
				t.Fatalf("artifacts = %v, error = %v", artifacts, err)
			}
			log, err := os.ReadFile(filepath.Join(artifacts[0], "test.log"))
			if err != nil || string(log) != "tester-output\n" {
				t.Errorf("test log = %q, error = %v", log, err)
			}
			savedStatus, err := os.ReadFile(filepath.Join(artifacts[0], "exit-status.txt"))
			if err != nil || string(savedStatus) != tt.testerStatus+"\n" {
				t.Errorf("saved status = %q, error = %v", savedStatus, err)
			}
		})
	}
}

func TestLicenseCollection(t *testing.T) {
	for _, tc := range []struct {
		name, failure string
		relocated     bool
	}{
		{name: "nested notices and duplicate modules"},
		{name: "packaged toolchain license", relocated: true},
		{name: "missing dependency license", failure: "license"},
		{name: "dependency lookup failure", failure: "list"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			module := filepath.Join(root, "module cache", "example.org", "library@v1.2.3")
			goroot := filepath.Join(root, "toolchain")
			bin := filepath.Join(root, "bin")
			write := func(path, content string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(module, 0755); err != nil {
				t.Fatal(err)
			}
			if tc.failure != "license" {
				write(filepath.Join(module, "LICENSE"), "dependency license\nunchanged bytes\n")
				write(filepath.Join(module, "nested", "NOTICE"), "nested attribution\n")
				// Go's module cache files are read-only; collecting twice must work.
				for _, name := range []string{"LICENSE", "nested/NOTICE"} {
					if err := os.Chmod(filepath.Join(module, name), 0444); err != nil {
						t.Fatal(err)
					}
				}
			}
			write(filepath.Join(module, "AUTHORS"), "dependency authors\n")
			write(filepath.Join(module, "source.go"), "not a notice\n")
			licenseRoot := goroot
			if tc.relocated {
				licenseRoot = filepath.Dir(goroot)
			}
			write(filepath.Join(licenseRoot, "LICENSE"), "toolchain license\n")
			write(filepath.Join(goroot, "PATENTS"), "toolchain patents\n")
			write(filepath.Join(goroot, "src", "vendor", "example", "COPYING.txt"), "vendored toolchain notice\n")
			fake := `#!/bin/sh
case "$1" in
 list)
  if [ "$LICENSE_TEST_FAILURE" = list ]; then echo 'dependency lookup failed' >&2; exit 23; fi
  printf '\nexample.org/library@v1.2.3\t%s\nexample.org/library@v1.2.3\t%s\n' "$LICENSE_TEST_MODULE" "$LICENSE_TEST_MODULE"
  ;;
 env) printf '%s\n' "$LICENSE_TEST_GOROOT" ;;
 version) printf '%s\n' 'go version test-toolchain' ;;
 *) exit 2 ;;
esac
`
			write(filepath.Join(bin, "go"), fake)
			if err := os.Chmod(filepath.Join(bin, "go"), 0755); err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(root, "collected notices")
			runCollector := func() ([]byte, error) {
				cmd := exec.Command("/bin/sh", "../../scripts/collect-licenses.sh", destination, ".")
				cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "LICENSE_TEST_MODULE="+module, "LICENSE_TEST_GOROOT="+goroot, "LICENSE_TEST_FAILURE="+tc.failure)
				return cmd.CombinedOutput()
			}
			output, err := runCollector()
			if tc.failure != "" {
				want := "No license files found"
				if tc.failure == "list" {
					want = "dependency lookup failed"
				}
				if err == nil || !strings.Contains(string(output), want) {
					t.Fatalf("collector error=%v output=%s, want %q", err, output, want)
				}
				return
			}
			if err != nil {
				t.Fatalf("collector: %v\n%s", err, output)
			}
			if output, err := runCollector(); err != nil {
				t.Fatalf("collecting again: %v\n%s", err, output)
			}
			projectLicense, err := os.ReadFile("../../LICENSE")
			if err != nil {
				t.Fatal(err)
			}
			for name, want := range map[string]string{
				"LICENSE":     string(projectLicense),
				"modules.txt": "example.org/library@v1.2.3\n",
				"modules/example.org/library@v1.2.3/LICENSE":       "dependency license\nunchanged bytes\n",
				"modules/example.org/library@v1.2.3/nested/NOTICE": "nested attribution\n",
				"go/LICENSE":                        "toolchain license\n",
				"go/PATENTS":                        "toolchain patents\n",
				"go/src/vendor/example/COPYING.txt": "vendored toolchain notice\n",
				"go/VERSION":                        "go version test-toolchain\n",
			} {
				data, err := os.ReadFile(filepath.Join(destination, name))
				if err != nil || string(data) != want {
					t.Errorf("%s=%q (%v), want %q", name, data, err, want)
				}
			}
			if _, err := os.Stat(filepath.Join(destination, "modules", "example.org/library@v1.2.3", "source.go")); !os.IsNotExist(err) {
				t.Fatalf("collector copied unrelated source: %v", err)
			}
		})
	}
}

func TestRunnerPlatform(t *testing.T) {
	for _, tc := range []struct {
		name, daemon, override, image, want string
		infoFailure                         bool
		wantStatus                          int
	}{
		{name: "native ARM64", daemon: "linux/aarch64", image: "linux/arm64", want: "linux/arm64"},
		{name: "canonical ARM64", daemon: "linux/arm64", image: "linux/arm64", want: "linux/arm64"},
		{name: "native AMD64", daemon: "linux/x86_64", image: "linux/amd64", want: "linux/amd64"},
		{name: "canonical AMD64", daemon: "linux/amd64", image: "linux/amd64", want: "linux/amd64"},
		{name: "explicit AMD64", daemon: "linux/aarch64", override: "linux/amd64", image: "linux/amd64", want: "linux/amd64"},
		{name: "explicit ARM64", daemon: "linux/x86_64", override: "linux/arm64", image: "linux/arm64", want: "linux/arm64"},
		{name: "unsupported daemon", daemon: "linux/s390x", wantStatus: 1},
		{name: "unsupported override", override: "linux/arm/v7", wantStatus: 1},
		{name: "daemon failure", infoFailure: true, wantStatus: 1},
		{name: "image mismatch", daemon: "linux/aarch64", image: "linux/amd64", want: "linux/arm64", wantStatus: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			fake := `#!/bin/sh
case "$1" in
 info)
  [ "$PLATFORM_INFO_FAILURE" != true ] || exit 1
  echo "$PLATFORM_DAEMON"
  exit 0
  ;;
 image) echo "$PLATFORM_IMAGE" ;;
 network)
  if [ "$2" = inspect ] && [ "$3" = --format ]; then echo false/true/false; fi
  ;;
 compose)
  shift 5
  case "$1" in
   config) echo "$SZTP_DHCP_PLATFORM" > "$PLATFORM_SELECTED" ;;
   up) touch "$PLATFORM_STARTED" ;;
  esac
  ;;
esac
`
			if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(fake), 0755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "run.sh")
			cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "TMPDIR="+dir,
				"SZTP_DHCP_PLATFORM="+tc.override, "PLATFORM_DAEMON="+tc.daemon, "PLATFORM_IMAGE="+tc.image,
				"PLATFORM_INFO_FAILURE="+map[bool]string{true: "true", false: "false"}[tc.infoFailure],
				"PLATFORM_SELECTED="+filepath.Join(dir, "selected"), "PLATFORM_STARTED="+filepath.Join(dir, "started"))
			output, err := cmd.CombinedOutput()
			status := 0
			if err != nil {
				if e, ok := err.(*exec.ExitError); ok {
					status = e.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if status != tc.wantStatus {
				t.Fatalf("status %d, want %d: %s", status, tc.wantStatus, output)
			}
			selected, _ := os.ReadFile(filepath.Join(dir, "selected"))
			if strings.TrimSpace(string(selected)) != tc.want {
				t.Errorf("platform %q, want %q", selected, tc.want)
			}
			_, started := os.Stat(filepath.Join(dir, "started"))
			if tc.wantStatus != 0 && !os.IsNotExist(started) {
				t.Error("started fixture after platform failure")
			}
		})
	}
}
