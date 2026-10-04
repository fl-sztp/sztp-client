#!/usr/bin/env bash

# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 Anssi Söderena
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -Eeuo pipefail

usage() {
    printf 'Usage: %s [--quick] [--case NAME] [--help]\nFull suite: 76 cases. Quick suite: 73 cases; omits real timeout/retry checks.\n' "$0"
}
# Keep fixed command words in the array: Bash 3.2 rejects empty arrays under set -u.
tester_command=(run --rm --no-deps tester tester)
quick_seen=0 case_seen=0 help_seen=0
while (( $# )); do
    case "$1" in
        --quick)
            (( quick_seen == 0 )) || { echo "Repeated --quick" >&2; exit 2; }
            quick_seen=1
            tester_command+=(-test.short)
            shift
            ;;
        --case)
            (( case_seen == 0 )) || { echo "Repeated --case" >&2; exit 2; }
            if (( $# < 2 )) || [[ -z "$2" || "$2" == -* ]]; then
                echo "--case requires a nonempty case name" >&2; exit 2
            fi
            case_seen=1
            tester_command+=("-dhcp.case=$2")
            shift 2
            ;;
        --help)
            (( help_seen == 0 )) || { echo "Repeated --help" >&2; exit 2; }
            help_seen=1
            shift
            ;;
        *) echo "Unexpected argument: $1" >&2; usage >&2; exit 2 ;;
    esac
done
if (( help_seen )); then usage; exit 0; fi

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
export SZTP_DHCP_REPOSITORY
SZTP_DHCP_REPOSITORY=$(cd -- "$script_dir/../.." && pwd -P)
export SZTP_DHCP_ARTIFACTS
artifact_parent=${TMPDIR:-/tmp}
SZTP_DHCP_ARTIFACTS=$(mktemp -d "${artifact_parent%/}/sztp-dhcp.XXXXXXXX")
export SZTP_DHCP_PROJECT
SZTP_DHCP_PROJECT=$(basename "$SZTP_DHCP_ARTIFACTS" | tr '[:upper:].' '[:lower:]-')
export SZTP_DHCP_IMAGE="$SZTP_DHCP_PROJECT:local"
compose=(docker compose --project-name "$SZTP_DHCP_PROJECT" --file "$script_dir/compose.yaml")
project_started=0
active_command_pid=
active_tee_pid=
active_pipe=

# Keep the command's status if tee also fails, and fail on a lost log otherwise.
run_logged() {
    local log=$1
    shift
    local command_status=0 tee_status=0
    active_pipe="$SZTP_DHCP_ARTIFACTS/.$log.pipe"
    mkfifo "$active_pipe"
    tee "$SZTP_DHCP_ARTIFACTS/$log" <"$active_pipe" &
    active_tee_pid=$!
    "$@" >"$active_pipe" 2>&1 &
    active_command_pid=$!
    # Bash wait reacts to signals immediately; waiting on a foreground pipeline
    # would postpone the cleanup trap until the Docker process returned.
    if wait "$active_command_pid"; then :; else command_status=$?; fi
    active_command_pid=
    if wait "$active_tee_pid"; then :; else tee_status=$?; fi
    active_tee_pid=
    rm "$active_pipe"
    active_pipe=
    if (( command_status != 0 )); then return "$command_status"; fi
    return "$tee_status"
}

cleanup() {
    local status=$?
    local cleanup_failed=0 containers networks
    trap - EXIT INT TERM
    set +e
    if [[ -n "$active_command_pid" ]]; then
        kill -TERM "$active_command_pid" 2>/dev/null
        wait "$active_command_pid" 2>/dev/null
    fi
    if [[ -n "$active_tee_pid" ]]; then
        wait "$active_tee_pid" || cleanup_failed=1
    fi
    if [[ -n "$active_pipe" ]]; then
        rm -f "$active_pipe" || cleanup_failed=1
    fi
    if (( project_started )); then
        "${compose[@]}" logs --no-color --timestamps >"$SZTP_DHCP_ARTIFACTS/compose.log" 2>&1 || cleanup_failed=1
        "${compose[@]}" logs --no-color --timestamps fixture >"$SZTP_DHCP_ARTIFACTS/fixture.log" 2>&1 || cleanup_failed=1
        # Per-case events and client logs are saved by the driver. Preserve the
        # fixture's final snapshot too, including failures before the driver.
        "${compose[@]}" exec -T fixture wget -q -O - http://127.0.0.1:8081/events >"$SZTP_DHCP_ARTIFACTS/final-events.json" 2>"$SZTP_DHCP_ARTIFACTS/final-events-error.log" || cleanup_failed=1
        "${compose[@]}" down --remove-orphans >"$SZTP_DHCP_ARTIFACTS/cleanup.log" 2>&1 || cleanup_failed=1
        containers=$(docker ps -aq --filter "label=com.docker.compose.project=$SZTP_DHCP_PROJECT") || cleanup_failed=1
        networks=$(docker network ls -q --filter "label=com.docker.compose.project=$SZTP_DHCP_PROJECT") || cleanup_failed=1
        if [[ -n "$containers" || -n "$networks" ]]; then
            printf 'Project resources remain: containers=%s networks=%s\n' "$containers" "$networks" >>"$SZTP_DHCP_ARTIFACTS/cleanup.log"
            cleanup_failed=1
        fi
    fi
    if (( cleanup_failed )); then
        echo "Artifact capture or project cleanup failed; see retained logs." >&2
        if (( status == 0 )); then status=1; fi
    fi
    printf 'Exit status: %s\nArtifacts: %s\nProject: %s\n' "$status" "$SZTP_DHCP_ARTIFACTS" "$SZTP_DHCP_PROJECT"
    printf '%s\n' "$status" >"$SZTP_DHCP_ARTIFACTS/exit-status.txt"
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

printf 'Artifacts: %s\nProject: %s\n' "$SZTP_DHCP_ARTIFACTS" "$SZTP_DHCP_PROJECT"
"${compose[@]}" config >"$SZTP_DHCP_ARTIFACTS/compose-config.yaml"
run_logged build.log "${compose[@]}" build fixture
architecture=$(docker image inspect --format '{{.Os}}/{{.Architecture}}' "$SZTP_DHCP_IMAGE")
printf '%s\n' "$architecture" >"$SZTP_DHCP_ARTIFACTS/image-platform.txt"
if [[ "$architecture" != linux/amd64 ]]; then
    echo "Built image platform is $architecture; require linux/amd64" >&2
    exit 1
fi
project_started=1
run_logged startup.log "${compose[@]}" up -d --wait --wait-timeout 30 fixture
docker network inspect "${SZTP_DHCP_PROJECT}_dhcp" >"$SZTP_DHCP_ARTIFACTS/network.json"
isolation=$(docker network inspect --format '{{.Internal}}/{{.EnableIPv6}}/{{index .Options "com.docker.network.bridge.enable_ip_masquerade"}}' "${SZTP_DHCP_PROJECT}_dhcp")
printf '%s\n' "$isolation" >"$SZTP_DHCP_ARTIFACTS/network-isolation.txt"
if [[ "$isolation" != false/true/false ]]; then
    echo "Require dual-stack DHCP bridge with masquerading disabled; got $isolation" >&2
    exit 1
fi
run_logged test.log "${compose[@]}" "${tester_command[@]}"
