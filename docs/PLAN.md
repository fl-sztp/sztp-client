<!--
SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
SPDX-License-Identifier: Apache-2.0
-->

# Development Plan

Tasks follow increasing effort, not urgency; dependencies take precedence. [Release preparation](#release-preparation) and device support are independent; [completed work](#completed-work) follows last.

| Order | Open task | Dependency or boundary |
|---|---|---|
| 1 | [Documentation corrections](#documentation-corrections) | Describe current behavior. |
| 2 | [Script startup cancellation](#provisioning-script-startup-cancellation) | Does not terminate running scripts. |
| 3 | [Human-readable HTTP response sizes](#human-readable-http-response-sizes) | Extend completed HTTP limits. Preserve default byte counts. |
| 4 | [Manual page](#command-line-manual-page) | Document implemented behavior. |
| 5 | [Go 1.26 compatibility](#go-126-compatibility) | Validate before lowering the minimum. |
| 6 | [TLS credential validation](#tls-credential-validation) | Preserve mode-specific behavior. |
| 7 | [CI workflows](#ci-workflows-and-local-validation) | Start on Go 1.27. Add 1.26 after compatibility work. |
| 8 | [Bootstrap diagnostics](#bootstrap-error-diagnostics) | Reuse completed HTTP limits. |
| 9 | [DHCP retry and cancellation](#dhcp-retry-and-cancellation) | Preserve explicit-server precedence. |
| 10 | [NetworkManager compatibility](#networkmanager-compatibility) | Gather evidence before choosing integration. |
| 11 | [Device-specific configuration and boot images](#device-specific-configuration-and-boot-images) | Shared architecture before real BF3 handlers. |

## Release Preparation

Support the [Fedora review](https://bugzilla.redhat.com/show_bug.cgi?id=2441999). NetworkManager/retry/startup follow-ups reflect correspondence/source inspection, not Fedora requirements. Reviewer recruitment, correspondence, spec/SRPM work stays with Bugzilla/the packager.

**Linux checks** mean these commands as an ordinary user, with CGO and a C compiler:

```sh
go test -mod=readonly ./...
go vet -mod=readonly ./...
go test -mod=readonly -race ./...
```

### Documentation Corrections

Document current code/help without behavior/API changes.

- [ ] **Exit statuses:** In [usage.md](usage.md), explain 0 = success, 1 = application error (including incompatible flags), 2 = CLI parsing error. Distinguish discovery failure from report-writing/other errors.
- [ ] **TLS:** Host-meta/POST discard credential-loading errors; final reporting checks certificates/key and trust anchor. Explain local failures appearing as bootstrap network retries; link [planned validation](#tls-credential-validation).
- [ ] **Redirects/scripts:** First redirect only; its trust anchor is ignored, configured credentials/anchor retained. Context cancellation does not terminate running scripts; distinguish direct child signals sent by systemd/process management.
- [ ] **Coverage/CI:** Verify 89 full / 85 quick cases, unit tests in every mode and REUSE versus [planned CI](#ci-workflows-and-local-validation). Change [development.md](development.md) only for proven catalog/runner/workflow discrepancies.

[DHCP lab preparation](development.md#test-dhcp-discovery-with-docker) is complete. Accept accurate guides, working links, `reuse lint` and changed-document `reuse lint-file` passes. Preserve notices and report unresolved ownership.

### Provisioning Script Startup Cancellation

`runConfiguration` can launch after cancellation during its start report.

- [ ] Recheck `ctx.Err()` after reporting, immediately before pre/post execution. Return context errors, preserving cancellation/deadline `errors.Is` through bootstrap wrapping.
- [ ] Keep active-context reporting best effort: report failure alone must not block scripts.
- [ ] Cancel deterministically in each phase's start-report handler, without sleeps. Assert no phase execution marker, bootstrap cancellation errors and no completion. Pre may finish before post cancellation.
- [ ] Retain uncanceled success/failed-report tests; distinguish startup checks from running-script termination.

Accept both regressions and Linux checks. Running-script termination, process groups and rollback remain outside scope.

### Human-Readable HTTP Response Sizes

Extend all three [response limits](usage.md#bound-http-requests) in YAML/CLI, preserving integer bytes. Use a shared standard-library parser with exact decimal arithmetic.

- [ ] Add internal `parseByteSize(string) (int64, error)` to `resolveHTTPRequestLimits` in `init.go`; retain raw YAML until explicit CLI overrides.
- [ ] Accept optional `+`, digits with optional `.digits`, case-insensitive suffixes, surrounding whitespace and whitespace before units. No suffix/`B` means bytes; `K/M/G/T` and `KB/MB/GB/TB` mean powers of 1000, `KiB/MiB/GiB/TiB` powers of 1024. Reject commas, exponents, fractions (`1/2MiB`) and unknown units.
- [ ] Validate syntax before exact multiplication with [`math/big.Rat`](https://pkg.go.dev/math/big#Rat). Require positive integral `int64`, without rounding/floats; reject `0.1KiB` and unitless `1.5`. Retain `HTTPRequestLimits.Validate()`, architecture bounds and overflow-detection extra byte.
- [ ] Preserve defaults (1 MiB, 16 MiB, 64 KiB), omitted/null defaults, CLI precedence over invalid YAML scalars, zero/negative rejection, public `HTTPRequestLimits` fields as `int64` and deadline/body-reading contracts. Errors identify options and valid examples.
- [ ] Update help, [usage](usage.md), [development](development.md#http-request-limits), `config.yaml.example` and planned man page. Show defaults as `1MiB`/`16MiB`/`64KiB`, exact bytes in help, SI/IEC and whole-byte rules; use `--bootstrap-max-response-bytes 32MiB` in usage.
- [ ] Test all settings via CLI and quoted/unquoted YAML: valid CLI over invalid YAML, case/whitespace, `+`, legacy integers and omitted/null defaults. Assert:

  | Input | Bytes |
  |---|---:|
  | `1048576` | 1048576 |
  | `1M` / `1MB` | 1000000 |
  | `2G` | 2000000000 |
  | `1MiB` | 1048576 |
  | `1.5MiB` | 1572864 |

- [ ] Test malformed/empty/zero/negative values, unknown units, fractional bytes, overflow, architecture boundaries, exact integers above `2^53` on 64-bit and help/default agreement. Retain response-boundary tests.

Accept focused regressions, Linux checks, matching help/examples and `reuse lint` with attribution preserved. Extends [completed limits](#http-request-limits); remains pending validation.

### Command-Line Manual Page

- [ ] Add English `docs/man/sztp-client.1`, tracked roff with SPDX, no generator: NAME, SYNOPSIS, DESCRIPTION, OPTIONS, FILES, ENVIRONMENT, NOTES, EXAMPLES, SEE ALSO.
- [ ] Cover all flags/defaults/exclusions, YAML/CLI precedence, credentials and relative paths; distinguish `./config.yaml` from `/etc/sztp-client/config.yaml`.
- [ ] Cover state/reset, pending reports, cleanup, script working directories, privileges, stdout/stderr, protocol limits, exit statuses 0/1/2 and discovery success. Give implemented examples for all three modes.
- [ ] Add startup/mandatory-credential instructions and `man sztp-client` to `--help`, matching [deployment](deployment.md#install-a-systemd-service).
- [ ] Follow [installation rules](development.md#installed-files-and-service-integration). Check `mandoc -Tlint`, `man -l`, help agreement, `reuse lint`, installed `man sztp-client` and rebuilt-RPM `rpmlint`.

Accept repository-independent configuration, result interpretation and safe reprovisioning.

### Go 1.26 Compatibility

Target `go 1.26.0` with current dependencies; no target-host Go requirement or restriction on newer patched compilers. See [investigation](development.md#go-126-compatibility-investigation).

- [ ] Check `utf8.ValidString(hostname)` before IDNA in `serveraddr.normalizeHostname`; preserve hostname errors and valid Unicode.
- [ ] Retain malformed UTF-8 regression; verify normalization/DHCP parsing on Go 1.26 and 1.27.
- [ ] Update module minimum, README and development guide. Pin the Docker test builder to an available, verified, security-patched Go 1.26 image.
- [ ] Extend [CI](#ci-workflows-and-local-validation) to both versions without automatic toolchain upgrades once the fix/minimum are ready.

Accept Linux checks on both versions, Linux/amd64 and Linux/arm64 builds, and all 89 Docker cases with the updated builder before lowering the minimum.

### TLS Credential Validation

Bootstrap discards loading errors while final reporting checks them, hiding local failures behind retries or detecting them after provisioning.

- [ ] Share checked certificate/key/trust-anchor loading across host-meta, bootstrap and progress/resumed reporting. Return read/`tls.LoadX509KeyPair` errors; reject `AppendCertsFromPEM` finding no certificates.
- [ ] Stop local failures before HTTP/scripts and bootstrap retries; retain pending completion. Preserve `errors.Is` for `ErrReportCredentials` and file errors. Network failures stay retryable.
- [ ] `--insecure` skips server verification only. Preserve credential-free reset/completed startup and diagnostics that report credential problems while continuing DHCP.
- [ ] Test missing/unreadable files, malformed PEM, absent CA certificates, mismatched keys and valid credentials across bootstrap/resumed reporting, with/without `--insecure`. Invalid credentials cause no HTTP/scripts even against unauthenticated servers. Run permission tests as ordinary user.
- [ ] Identify failed inputs without keys/passwords/file contents; document shared validation and terminal local errors.

Accept focused tests/Linux checks while preserving mutual TLS, pending recovery, HTTP limits/deadlines and documented modes.

### CI Workflows and Local Validation

Keep [REUSE](../.github/workflows/reuse.yml); add `.github/workflows/ci.yml`. Both use pull requests/`workflow_dispatch`; start Go 1.27 independently of [1.26](#go-126-compatibility).

- [ ] Create four independent Ubuntu 24.04 jobs:

  | Job | Checks |
  |---|---|
  | Formatting/workflows | Fail on nonempty `gofmt -l` without rewriting. Run [actionlint](https://github.com/rhysd/actionlint/blob/main/docs/checks.md) on every workflow. |
  | Linux tests | Run the Linux checks above as an ordinary user, with CGO/C compiler. |
  | Linux builds | Static Linux/amd64 and Linux/arm64 builds with [documented settings](deployment.md#build-a-linux-binary). |
  | DHCP integration | Full `bash docker-tests/integration/run.sh`, no `--quick`, using the existing Docker builder. |

- [ ] Use latest Go 1.27 patch for host jobs, `GOTOOLCHAIN=local`, recording resolved versions. Add 1.26 after its fix/minimum update.
- [ ] Pin actions to verified commits; set `contents: read` and job timeouts.
- [ ] Set DHCP `TMPDIR` to runner temp; upload `sztp-dhcp.*` even on failure, preserving exit status. Root integration cannot replace ordinary-user permission tests.
- [ ] Align local/CI commands; document commands, versions, runner images, architectures and prerequisites in [development](development.md#run-unit-tests-and-static-checks).
- [ ] Run local actionlint/Linux/full Docker checks, then actual GitHub jobs. Optional [act](https://nektosact.com/usage/index.html) requires documenting [runner differences](https://nektosact.com/usage/runners.html)/[unsupported features](https://nektosact.com/not_supported.html).

Accept local/GitHub passes, both architectures and full DHCP catalog, without root-related skips (intentional subprocess-helper skips allowed). Inject formatting/test/DHCP failures: jobs fail and artifacts remain downloadable. Leave tracked files unchanged; record environments/commands/results. Application/API changes and branch protection are outside scope.

### Bootstrap Error Diagnostics

Audit M1: retain useful reasons without raw bodies in errors/logs/progress. Raw logs/unbounded reads were inherited; current changes also propagate bodies into errors/progress.

- [ ] In `sztpclient/request.go`, replace raw-body concatenation with bounded diagnostics: HTTP status, safe parsing errors and approved validation details; unknown formats get generic diagnostics.
- [ ] Keep reasons such as `post_configuration_script` length errors. Allowed field names cannot authorize arbitrary untrusted server values.
- [ ] Exclude credentials/configuration/scripts throughout errors, progress and logs, including existing raw-response logging.
- [ ] Define/document error/report diagnostic-length limits; reuse [M2 limits](#http-request-limits), without another reader or competing transport bounds.
- [ ] Update `TestBootstrapReportsOriginalValidationError` in `onboarding_bootstrap_test.go` to require reason without body. Test sensitive content, unknown JSON, HTML and below/at/above limits in errors/logs/reports; retain M2 boundary tests.

Accept focused/Linux passes: reasons survive, sensitive data is absent and oversized responses fail without unbounded buffering.

### DHCP Retry and Cancellation

Fix [discovery limitations](development.md#dhcp-sockets).

- [ ] Propagate cancellation through both protocols/waits. Test `SIGTERM` during each query/between rounds: prompt stop without normal delays, sockets released.
- [ ] Rediscover until onboarding succeeds when no server is explicit; initial failures/fallback must not disable discovery. Preserve explicit-server precedence.
- [ ] Enable DHCP after initial rounds; the same process must discover/use it.

Accept prompt cancellation/recovery without restart; update discovery/fallback documentation.

### NetworkManager Compatibility

Extend [coverage](development.md#networkmanager-coverage) with evidence.

- [ ] Build repeatable Fedora setup with a NetworkManager-managed interface; separately test DHCPv4 option 143 and DHCPv6 option 136 alongside address acquisition/renewal.
- [ ] Document Fedora/NetworkManager/client versions, setup, commands/results; distinguish socket errors, missing options and missing responses.
- [ ] Plan from failures; verify NetworkManager requests/exposes SZTP options before choosing D-Bus/dispatcher integration.

Accept both protocols discovering expected servers without disrupting acquisition/renewal. Document limitations until verified; no plugin/GUI/MOTD commitment.

## Device-Specific Configuration and Boot Images

### Scope and Interfaces

Client responsibilities: transfer, hardware/OS detection, independent configuration/image handler selection, order and reporting. Compile separate Go interfaces; authentication serials alone do not identify families.

Build interfaces/selection/reporting/tests before BF3 handlers/installers. Initial configuration must be verifiable/reversible without reboot. GRUB, firmware and image download/install/reboot need separate device designs. One-time onboarding only.

- Configuration handlers validate compatibility/settings, prepare `Commit`/`Rollback`/`Finalize` transactions and recover from persistent data.
- Image handlers check running images, verify/install replacements; matching images need no install. Start with a test handler.
- Profiles describe OS/BFB versions, capabilities/differences; platform and configuration-model versions remain separate.

Retain RFC envelope; migrate free-form Base64 configuration to shared JSON containing model ID/version/settings. Models define ownership/deletion: `merge` combines within handler scope, `replace` replaces that scope, never generic JSON merge. Initially handlers perform all owned changes.

Handlers may use OPI/OVSDB/Linux tools. [OPI API](https://github.com/opiproject/opi-api)/[NVIDIA bridge](https://github.com/opiproject/opi-nvidia-bridge) are candidates, not complete coverage.

### Behavior and Lifecycle

Validate before changes; select configuration/image support independently:

| Situation | Action |
|---|---|
| No content | Skip without warning. |
| Unidentified device | Skip with unknown-device warning. |
| Known device, no handler/compatible profile | Skip with missing-support warning. |
| Well-formed unsupported configuration model/version | Skip configuration with specific reason. |
| One compatible handler, valid content | Execute and verify. |
| Malformed payload, ambiguous selection, supported failure | Stop with error and roll back configuration where applicable. |

Device names do not prove support. No image downloads on skips or script substitution for unsupported configuration. Optional configuration still requires a valid model envelope and supported `configuration-handling` when supplied.

1. Validate payload, detect device, select support.
2. Process boot image or report skip.
3. Run optional pre script.
4. Recheck configuration prerequisites and prepare transaction.
5. Persist recovery before the first configuration change.
6. Commit/verify configuration or report skip.
7. Run optional post script.
8. Persist completion including skips and finalize transaction.

Transactions cover supported configuration only; boot precedes scripts, commit precedes post script ([RFC 8572 §5.6](https://www.rfc-editor.org/rfc/rfc8572.html#section-5.6)).

### Completion and Recovery

- `config-complete` requires committed configuration; `boot-image-complete` requires a verified running image, not installer success before reboot.
- Unsupported operations emit `config-warning`/`boot-image-warning` with operation/device/reason, never success. Persist skips/reasons in journal/completion; include them in `bootstrap-complete` across retries/restarts. Separate local outcome from acknowledgement.
- Otherwise successful onboarding with skips completes with warnings. Added support does not repeat provisioning without reset.
- This deliberately departs from RFC 8572's successful processing of all supplied criteria before `bootstrap-complete`. Preserve wire format, document departure, do not claim full compliance ([§5.6](https://www.rfc-editor.org/rfc/rfc8572.html#section-5.6), [§7.3](https://www.rfc-editor.org/rfc/rfc8572.html#section-7.3)).
- Journal in existing state directory; restore handler scope on commit/post failure. Recover before retry; stop changes on recovery failure. Journaled success with failed completion persistence must finish persistence/finalization without repeating scripts.
- Rollback covers handler-owned configuration only; authors own script side effects/cleanup/reruns. Arbitrary shell commands/image replacement are outside guarantees.

### Acceptance

Test configuration/image handlers for:

- Every table case, configuration-only/image-only support and matching images.
- Scripts with missing content; both skips plus successful scripts complete with warnings, never false success.
- Malformed/ambiguous input stopping before changes; supported failures remaining errors.
- Lifecycle order, commit/post rollback and model-defined merge/replace boundaries.
- Interrupted recovery, stopping on recovery failure, completion-write recovery without repeated scripts.
- Durable skip reasons across recovery/retries/restarts; new support requires reset to reprovision.

Accept adding either capability without SZTP-layer changes. Real handlers/installers need hardware acceptance.

## Completed Work

### Bootstrap Redirect Address Validation

MEDIUM finding implemented separately from HTTP limits; upstream concatenation broke IPv6 and only checked positive separate ports.

- [x] `serveraddr.Normalize`/`net.JoinHostPort`: bare IPv6 and HTTP/HTTPS become HTTPS origins; validate embedded ports before authoritative separate port (1..65535).
- [x] Reject before URL changes/requests; preserve first-entry and one-redirect limits.
- [x] Test IPv4, bare/bracketed IPv6, hostnames, Unicode, URLs, invalid addresses and missing/negative/zero/oversized ports plus boundaries.
- [x] Real IPv4-to-IPv6 TLS redirect: scripts once, pending-report resume against saved bracketed IPv6 endpoint.

Validated 2026-10-22: Linux/arm64, Go 1.27.1, UID 65534, all Linux checks. IPv6 regression ran without skip and reproduced the pre-fix invalid-port error. Focused macOS/arm64 tests passed on Go 1.27.2; `reuse lint`/changed-file `reuse lint-file` passed, notices retained.

### HTTP Request Limits

Audit M2 implemented: [independent host-meta/bootstrap/progress bounds](usage.md#bound-http-requests), including redirects/resumed reports. M1 remains open.

- [x] YAML/CLI timeouts/sizes; explicit CLI overrides before validation; reject zero/negative/malformed/overflow.
- [x] Configurable operational defaults, not protocol requirements: host-meta 30 s / 1 MiB, bootstrap 120 s / 16 MiB, progress 30 s / 64 KiB.
- [x] Child-context deadlines through reading, preserving earlier deadlines/cancellation; bound decompressed responses, rejecting oversized contents without retaining them.
- [x] Preserve timeout/cancellation/size-error identities through bootstrap wrapping; retain retries, workspace cleanup and persistent completion.
- [x] Test stalls, earlier deadlines/cancellation, fixed/chunked/gzip boundaries, bounded reads, precedence, pre-script retry recovery and pending-report limits.

Validated Linux/arm64, Go 1.27.1, ordinary user, all Linux checks. No overall onboarding deadline; DHCP/script cancellation remain separate.
