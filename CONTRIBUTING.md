<!--
SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
SPDX-License-Identifier: Apache-2.0
-->

# Contributing

This document is the canonical copyright and licensing policy for sztp-client.
Original contributions use Apache-2.0; see [LICENSE](LICENSE) and
[LICENSES/Apache-2.0.txt](LICENSES/Apache-2.0.txt). Contributors retain their rights
unless assigned elsewhere, such as to an employer. Contributing does not require
transferring copyright to a maintainer or company.

## Copyright ownership

Preserve existing copyright holders, years, upstream attribution, and notices
limiting ownership to modifications. Add holders or years only when supported by
the contribution and known ownership. Do not extend years for metadata edits or
calendar changes. Git authorship, profile links, and AI tool use alone do not
establish ownership. Review AI-assisted contributions for provenance and copied
material as you would other contributions.

## Derived and vendored material

Before copying or adapting material, record its source and revision and check its
license, copyright notices, and compatibility. Retain its original license
expression, required notices and attributions, and applicable distribution terms.
Provide required license texts under `LICENSES/` using their SPDX identifiers.
Preserve required NOTICE files and mark local modifications where required.

## Unknown copyright holders

Make a reasonable effort to identify the rightsholder from the source and
available history. If the holder remains unknown, use `NOASSERTION` and briefly
record the known source, checks made, and remaining uncertainty alongside the
file's metadata. Keep known holders and modification notices; replace
`NOASSERTION` when reliable information becomes available. The license must still
be established independently. Use `NONE` only when inspection supports an absence
of copyrightable content, and document that reason.

## SPDX metadata

Follow [REUSE Specification 3.3](https://reuse.software/spec-3.3/). Prefer
`SPDX-FileCopyrightText` and `SPDX-License-Identifier` comment headers near the
start of files, preserving shebangs, build constraints, and other format rules.
Where comments are unsuitable, use narrowly scoped `REUSE.toml` annotations or
adjacent `.license` files without hiding existing notices.

Keep headers concise. Put modification scope in the applicable copyright notice
(e.g. `(modifications)`), and use a short change notice when needed. Omit internal
file-move history and descriptions of individual changes.

Do not modify generated files solely to add metadata; inspect their sources and
generator instead. Keep license texts unchanged and secrets out of metadata.

## Validation

Run from the repository root:

```sh
reuse lint
```

Require exit status 0 and report unresolved ownership questions. Passing lint
checks metadata completeness, not ownership accuracy or license compatibility.
Do not hide missing metadata through exclusions. For licensing-only changes,
verify that other file content and behavior remain unchanged.
