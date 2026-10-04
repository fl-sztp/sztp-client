//go:build !dhcp_integration

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
			cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "TMPDIR="+dir, "RUNNER_CALLS="+filepath.Join(dir, "calls"), "RUNNER_ARGS="+filepath.Join(dir, "args"), "RUNNER_CLEANUP="+filepath.Join(dir, "cleanup"), "RUNNER_TESTER_STATUS="+tt.testerStatus)
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
