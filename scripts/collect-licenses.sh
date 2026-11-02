#!/bin/sh
# SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
# SPDX-License-Identifier: Apache-2.0

# Collect unchanged license and attribution files for distributed Go binaries.
# Dependency licenses may require these notices to accompany redistributions.
# Multi-stage Docker builds would otherwise leave them in the builder image.
# Run with the same GOOS, GOARCH, CGO_ENABLED, and build tags as the binaries.
set -eu

if [ "$#" -lt 2 ]; then
    echo "Usage: sh collect-licenses.sh OUTPUT_DIRECTORY [go list flags] PACKAGE..." >&2
    exit 2
fi

mkdir -p "$1"
destination=$(CDPATH= cd -- "$1" && pwd -P)
shift
project_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
cd "$project_root"

work=$(mktemp -d)
trap 'rm -rf "$work"' 0
trap 'exit 1' HUP INT TERM

# Keep the list command separate from sorting so a Go failure cannot be hidden
# by a successful pipeline. The final manifest contains no local cache paths.
go list -mod=readonly -deps -f '{{with .Module}}{{if not .Main}}{{.Path}}@{{.Version}}{{printf "\t"}}{{.Dir}}{{end}}{{end}}' "$@" >"$work/modules.raw"
LC_ALL=C sort -u "$work/modules.raw" >"$work/modules.sorted"
sed '/^$/d' "$work/modules.sorted" >"$work/modules"

copy_notices() {
    source_root=$1
    target_root=$2
    find "$source_root" -type f \( -iname 'LICENSE*' -o -iname 'COPYING*' \) >"$work/licenses"
    if [ ! -s "$work/licenses" ]; then
        echo "No license files found in $source_root" >&2
        exit 1
    fi
    find "$source_root" -type f \( -iname 'LICENSE*' -o -iname 'COPYING*' -o -iname 'NOTICE*' -o -iname 'AUTHORS*' -o -iname 'PATENTS*' \) >"$work/notices"
    while IFS= read -r notice; do
        relative=${notice#"$source_root"/}
        mkdir -p "$target_root/$(dirname -- "$relative")"
        cp -f "$notice" "$target_root/$relative"
    done <"$work/notices"
}

cp -f LICENSE "$destination/LICENSE"
for notice in COPYING NOTICE; do
    if [ -f "$notice" ]; then cp -f "$notice" "$destination/$notice"; fi
done

: >"$destination/modules.txt"
tab=$(printf '\t')
while IFS="$tab" read -r module directory; do
    if [ -z "$directory" ] || [ ! -d "$directory" ]; then
        echo "Module source directory is unavailable for $module" >&2
        exit 1
    fi
    copy_notices "$directory" "$destination/modules/$module"
    printf '%s\n' "$module" >>"$destination/modules.txt"
done <"$work/modules"

goroot=$(go env GOROOT)
go_license="$goroot/LICENSE"
# Packaged toolchains such as Homebrew place LICENSE beside the GOROOT directory.
if [ ! -f "$go_license" ] && [ -f "$goroot/../LICENSE" ]; then
    go_license="$goroot/../LICENSE"
fi
mkdir -p "$destination/go"
cp -f "$go_license" "$destination/go/LICENSE"
if [ -f "$goroot/PATENTS" ]; then cp -f "$goroot/PATENTS" "$destination/go/PATENTS"; fi
copy_notices "$goroot/src" "$destination/go/src"
go version >"$destination/go/VERSION"
