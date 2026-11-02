<!--
SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
SPDX-License-Identifier: Apache-2.0
-->

# Development Plan

Tasks follow increasing effort, not urgency. Dependencies take precedence. [Release preparation](#release-preparation) and device support are independent. [Completed work](#completed-work) follows last.

| Order | Open task | Dependency or boundary |
|---|---|---|
| 1 | [Documentation corrections](#documentation-corrections) | Describe current behavior. |
| 2 | [Unused onboarding timestamp field](#unused-onboarding-timestamp-field) | Assess compatibility before deciding removal. |
| 3 | [Script startup cancellation](#provisioning-script-startup-cancellation) | Does not terminate running scripts. |
| 4 | [Human-readable HTTP response sizes](#human-readable-http-response-sizes) | Extend completed HTTP limits. Preserve default byte counts. |
| 5 | [Manual page](#command-line-manual-page) | Document implemented behavior. |
| 6 | [Go 1.26 compatibility](#go-126-compatibility) | Validate before lowering the minimum. |
| 7 | [TLS credential validation](#tls-credential-validation) | Preserve mode-specific behavior. |
| 8 | [CI workflows](#ci-workflows-and-local-validation) | Start on Go 1.27. Add 1.26 after compatibility work. |
| 9 | [Bootstrap diagnostics](#bootstrap-error-diagnostics) | Reuse completed HTTP limits. |
| 10 | [DHCP retry and cancellation](#dhcp-retry-and-cancellation) | Preserve explicit-server precedence. |
| 11 | [NetworkManager compatibility](#networkmanager-compatibility) | Gather evidence before choosing integration. |
| 12 | [Docker server interoperability](#docker-sztp-server-interoperability) | Measure compatibility before client fixes. Extend planned CI. |
| 13 | [Device-specific configuration and boot images](#device-specific-configuration-and-boot-images) | Shared architecture before real BF3 handlers. |

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
- [ ] **TLS:** Host-meta/POST discard credential-loading errors. Final reporting checks certificates/key and trust anchor. Explain local failures appearing as bootstrap network retries. Link [planned validation](#tls-credential-validation).
- [ ] **Redirects/scripts:** First redirect only. Its trust anchor is ignored, configured credentials/anchor retained. Context cancellation does not terminate running scripts. Distinguish direct child signals sent by systemd/process management.
- [ ] **Coverage/CI:** Verify 89 full / 85 quick cases, unit tests in every mode and REUSE versus [planned CI](#ci-workflows-and-local-validation). Change [development.md](development.md) only for proven catalog/runner/workflow discrepancies.

[DHCP lab preparation](development.md#test-dhcp-discovery-with-docker) is complete. Accept accurate guides, working links, `reuse lint` and changed-document `reuse lint-file` passes. Preserve notices and report unresolved ownership.

### Unused Onboarding Timestamp Field

Assess whether `InfoTimestampReference` can be removed from `BootstrapServerOnboardingInfo` in `sztpclient/types.go`. Assessment and the removal decision remain open.

Repository inspection found only the field declaration, with no named reads or writes and no use in artifact naming, state management or completion reporting. No repository example or test requires the JSON key. Removing the old misleading comment did not remove the field.

Removal changes accepted input: JSON decoding can currently populate the field from `InfoTimestampReference`. The existing `DisallowUnknownFields()` in `sztpclient/client.go` would reject that key after removal. The field is also part of an exported Go structure, so external callers could depend on it. External compatibility has not been verified.

- [ ] Check whether supported bootstrap servers or known external Go callers require the field. Record the evidence and any remaining uncertainty.
- [ ] Record the compatibility assessment and an explicit removal decision. Repository evidence currently supports removal, subject to that assessment.
- [ ] If removal is approved, remove the field without a replacement or compatibility abstraction. Extend `TestOnboardingConveyedInformationValidation` to require rejection of `InfoTimestampReference` without scripts, leftover artifacts, persisted completion or completion reports. Retain valid onboarding cases and run Linux checks.

This entry records investigation and a possible later cleanup only. No application or test changes are part of the documentation task.

### Provisioning Script Startup Cancellation

Cancellation during a script's start report does not prevent the script from starting. `runConfiguration` in `sztpclient/configuration.go` checks the caller's context before sending the network-based start report, but continues to `cmd.Run()` if cancellation occurs during reporting. A new device-modifying pre- or post-configuration script can therefore start after a shutdown request. Remediation remains open.

- [ ] Recheck `ctx.Err()` after reporting, immediately before `cmd.Run()` in both phases. Return the context error without starting the script, preserving cancellation/deadline `errors.Is` through bootstrap wrapping.
- [ ] Keep start reporting best effort while the caller's context is active. A report failure alone must not block scripts.
- [ ] Cancel the caller's context deterministically in each phase's start-report server handler. Use synchronization rather than sleeps or timing-based waits.
- [ ] Assert with script-created execution markers that pre-phase cancellation starts neither script. For post-phase cancellation, allow pre to finish but assert that post never starts.
- [ ] Assert `context.Canceled` through the bootstrap error chain and no successful completion. Retain uncanceled success and failed-start-report regressions.

Accept both regressions and Linux checks. Running-script termination, process groups and rollback remain outside scope.

### Human-Readable HTTP Response Sizes

Extend all three [response limits](usage.md#bound-http-requests) in YAML/CLI, preserving integer bytes. Use a shared standard-library parser with exact decimal arithmetic.

- [ ] Add internal `parseByteSize(string) (int64, error)` to `resolveHTTPRequestLimits` in `init.go`. Retain raw YAML until explicit CLI overrides.
- [ ] Accept optional `+`, digits with optional `.digits`, case-insensitive suffixes, surrounding whitespace and whitespace before units. No suffix/`B` means bytes. `K/M/G/T` and `KB/MB/GB/TB` mean powers of 1000, `KiB/MiB/GiB/TiB` powers of 1024. Reject commas, exponents, fractions (`1/2MiB`) and unknown units.
- [ ] Validate syntax before exact multiplication with [`math/big.Rat`](https://pkg.go.dev/math/big#Rat). Require positive integral `int64`, without rounding/floats. Reject `0.1KiB` and unitless `1.5`. Retain `HTTPRequestLimits.Validate()`, architecture bounds and overflow-detection extra byte.
- [ ] Preserve defaults (1 MiB, 16 MiB, 64 KiB), omitted/null defaults, CLI precedence over invalid YAML scalars, zero/negative rejection, public `HTTPRequestLimits` fields as `int64` and deadline/body-reading contracts. Errors identify options and valid examples.
- [ ] Update help, [usage](usage.md), [development](development.md#http-request-limits), `config.yaml.example` and planned man page. Show defaults as `1MiB`/`16MiB`/`64KiB`, exact bytes in help, SI/IEC and whole-byte rules. Use `--bootstrap-max-response-bytes 32MiB` in usage.
- [ ] Test all settings via CLI and quoted/unquoted YAML: valid CLI over invalid YAML, case/whitespace, `+`, legacy integers and omitted/null defaults. Assert:

  | Input | Bytes |
  |---|---:|
  | `1048576` | 1048576 |
  | `1M` / `1MB` | 1000000 |
  | `2G` | 2000000000 |
  | `1MiB` | 1048576 |
  | `1.5MiB` | 1572864 |

- [ ] Test malformed/empty/zero/negative values, unknown units, fractional bytes, overflow, architecture boundaries, exact integers above `2^53` on 64-bit and help/default agreement. Retain response-boundary tests.

Accept focused regressions, Linux checks, matching help/examples and `reuse lint` with attribution preserved. Extends [completed limits](development.md#http-request-limits). Remains pending validation.

### Command-Line Manual Page

- [ ] Add English `docs/man/sztp-client.1`, tracked roff with SPDX, no generator: NAME, SYNOPSIS, DESCRIPTION, OPTIONS, FILES, ENVIRONMENT, NOTES, EXAMPLES, SEE ALSO.
- [ ] Cover all flags/defaults/exclusions, YAML/CLI precedence, credentials and relative paths. Document automatic `/etc/sztp-client/config.yaml` before `./config.yaml`, explicit-file selection and disabled file loading.
- [ ] Cover state/reset, pending reports, cleanup, script working directories, privileges, stdout/stderr, protocol limits, exit statuses 0/1/2 and discovery success. Give implemented examples for all three modes.
- [ ] Add startup/mandatory-credential instructions and `man sztp-client` to `--help`, matching [deployment](deployment.md#install-a-systemd-service).
- [ ] Follow [installation rules](development.md#installed-files-and-service-integration). Check `mandoc -Tlint`, `man -l`, help agreement, `reuse lint`, installed `man sztp-client` and rebuilt-RPM `rpmlint`.

Accept repository-independent configuration, result interpretation and safe reprovisioning.

### Go 1.26 Compatibility

Target `go 1.26.0` with current dependencies. No target-host Go requirement or restriction on newer patched compilers. See the [current Go requirement](development.md#run-unit-tests-and-static-checks).

- [ ] Check `utf8.ValidString(hostname)` before IDNA in `serveraddr.normalizeHostname`. Preserve hostname errors and valid Unicode.
- [ ] Retain malformed UTF-8 regression. Verify normalization/DHCP parsing on Go 1.26 and 1.27.
- [ ] Update module minimum, README and development guide. Pin the Docker test builder to an available, verified, security-patched Go 1.26 image.
- [ ] Extend [CI](#ci-workflows-and-local-validation) to both versions without automatic toolchain upgrades once the fix/minimum are ready.

Accept Linux checks on both versions, Linux/amd64 and Linux/arm64 builds, and all 89 Docker cases with the updated builder before lowering the minimum.

### TLS Credential Validation

Bootstrap ignores local TLS credential-loading failures. Host-meta and POST requests in `sztpclient/request.go` discard CA-file read errors, the `AppendCertsFromPEM` result and `tls.LoadX509KeyPair` errors. `ReportCompletion` in `sztpclient/progress.go` checks the same credentials. Remediation remains open.

Local configuration failures can therefore appear as indefinite network retries. If the server does not require a client certificate, provisioning can proceed with an invalid local key pair and fail only during final reporting.

- [ ] Share checked certificate/key/trust-anchor loading across host-meta, bootstrap, progress and resumed completion reporting. Return read/`tls.LoadX509KeyPair` errors. Reject `AppendCertsFromPEM` finding no certificates. Validate credentials before HTTP requests and provisioning scripts.
- [ ] Treat local credential configuration failures as terminal before bootstrap retries or reporting backoff. Retain pending completion. Preserve `errors.Is` for `ErrReportCredentials` and underlying file errors. Network failures stay retryable.
- [ ] `--insecure` skips server verification only, retaining local credential validation. Preserve credential-free reset/completed startup and diagnostics that report credential problems while continuing DHCP.
- [ ] Test missing/unreadable files, malformed PEM, absent CA certificates, mismatched keys and valid credentials across bootstrap and resumed completion reporting, with/without `--insecure`. Run permission tests as an ordinary user.
- [ ] Assert zero HTTP requests, script starts and retries for invalid credentials, including against a server that does not require a client certificate. Retain successful mutual TLS, pending-report recovery and credential-independent mode regressions.
- [ ] Identify failed inputs without passwords, private keys or file contents. Document shared validation and terminal local errors.

Accept focused tests/Linux checks while preserving mutual TLS, pending recovery, HTTP limits/deadlines and documented modes.

### CI Workflows and Local Validation

CI does not test application behavior. The only current workflows, [REUSE](../.github/workflows/reuse.yml) and [Workflow lint](../.github/workflows/workflow-lint.yml), check license metadata and GitHub Actions definitions. They neither build the client nor run Go tests, `go vet`, race tests or DHCP integration. A green CI result can therefore accompany a broken Linux implementation. Remediation remains open.

Keep both existing workflows and add `.github/workflows/ci.yml`, using pull requests/`workflow_dispatch`. Leave actionlint in the existing Workflow lint workflow rather than duplicating it in the new workflow. Start Go 1.27 independently of [1.26](#go-126-compatibility). Reuse the existing tests and Docker runner without introducing a new test framework.

- [ ] Create four independent Ubuntu 24.04 jobs:

  | Job | Checks |
  |---|---|
  | Formatting | Fail on nonempty `gofmt -l` without rewriting. |
  | Linux tests | Run `go test -mod=readonly ./...`, `go vet -mod=readonly ./...` and `go test -mod=readonly -race ./...` as an ordinary user, with CGO/C compiler. |
  | Linux builds | Static Linux/amd64 and Linux/arm64 builds with [documented settings](deployment.md#build-a-linux-binary). |
  | DHCP integration | Full `bash docker-tests/integration/run.sh`, no `--quick`, using the existing Docker builder. |

- [ ] Use latest Go 1.27 patch for host jobs, `GOTOOLCHAIN=local`, recording resolved versions. Add 1.26 after its fix/minimum update.
- [ ] Pin actions to verified commits. Set `contents: read` and job timeouts.
- [ ] Set DHCP `TMPDIR` to runner temp. Upload `sztp-dhcp.*` even on failure, preserving exit status. Root integration cannot replace ordinary-user permission tests.
- [ ] Align local/CI commands. Document commands, versions, runner images, architectures and prerequisites in [development](development.md#run-unit-tests-and-static-checks).
- [ ] Run local actionlint/Linux/full Docker checks, then actual GitHub jobs. Optional [act](https://nektosact.com/usage/index.html) requires documenting [runner differences](https://nektosact.com/usage/runners.html)/[unsupported features](https://nektosact.com/not_supported.html).

Accept local/GitHub passes, both architectures and full DHCP catalog, without root-related skips (intentional subprocess-helper skips allowed). Inject formatting/test/build/DHCP failures: the corresponding jobs fail and DHCP artifacts remain downloadable even on failure. Leave tracked files unchanged. Record environments/commands/results. Application/API changes and branch protection are outside scope.

### Bootstrap Error Diagnostics

Full bootstrap responses can reach errors, ordinary logs and progress reports. Remediation remains open. No actual disclosure incident was observed during this review.

When a response matches neither expected JSON structure, `postRequestWithContext` in `sztpclient/request.go` appends the entire body to its error. `RunBootstrapSequence` in `sztpclient/client.go` sends that error in a progress report, and `runBootstrapWithRetry` in `main.go` logs it at WARN. Responses containing configuration or credentials can therefore reach persistent logs. The default 16 MiB bootstrap response limit also permits substantial log volume. Debug logging additionally includes raw request, bootstrap and onboarding contents.

- [ ] Replace raw-body concatenation with bounded diagnostics that preserve HTTP status and safe parsing or validation reasons. Unknown formats get generic diagnostics.
- [ ] Retain useful reasons such as `post_configuration_script` length errors through approved diagnostics. Neither a known field name nor a server-provided error message authorizes echoing arbitrary untrusted values. Unknown field names may themselves contain sensitive content.
- [ ] Remove raw request/response and decoded bootstrap/onboarding contents from errors, progress reports and logs, including DEBUG. Preserve useful metadata without credentials, configuration or scripts.
- [ ] Define/document error/report diagnostic-length limits separately from transport bounds. Reuse [HTTP request limits](development.md#http-request-limits), preserving request deadlines and response-size limits without another reader or competing bounds.
- [ ] Change `TestBootstrapReportsOriginalValidationError` in `onboarding_bootstrap_test.go` to require a useful safe reason and HTTP status instead of the entire body. Use synthetic secrets and assert their absence from returned errors, WARN/DEBUG logs and progress reports.
- [ ] Cover expected error structures, unknown/malformed JSON, HTML and diagnostics below/at/above their length limits. Retain HTTP response-boundary tests.

Accept focused/Linux passes: HTTP status and safe reasons survive, synthetic secrets and raw contents are absent from every diagnostic sink, diagnostics remain bounded and oversized responses fail without unbounded buffering.

### DHCP Retry and Cancellation

Fix [discovery limitations](development.md#dhcp-sockets).

- [ ] Propagate cancellation through both protocols/waits. Test `SIGTERM` during each query/between rounds: prompt stop without normal delays, sockets released.
- [ ] Rediscover until onboarding succeeds when no server is explicit. Initial failures/fallback must not disable discovery. Preserve explicit-server precedence.
- [ ] Enable DHCP after initial rounds. The same process must discover/use it.

Accept prompt cancellation/recovery without restart. Update discovery/fallback documentation.

### NetworkManager Compatibility

Extend [coverage](development.md#networkmanager-coverage) with evidence.

- [ ] Build repeatable Fedora setup with a NetworkManager-managed interface. Separately test DHCPv4 option 143 and DHCPv6 option 136 alongside address acquisition/renewal.
- [ ] Document Fedora/NetworkManager/client versions, setup, commands/results. Distinguish socket errors, missing options and missing responses.
- [ ] Plan from failures. Verify NetworkManager requests/exposes SZTP options before choosing D-Bus/dispatcher integration.

Accept both protocols discovering expected servers without disrupting acquisition/renewal. Document limitations until verified. No plugin/GUI/MOTD commitment.

### Docker SZTP Server Interoperability

Measure current client compatibility with independent servers. Client fixes remain separate. The [existing Docker suite](development.md#test-dhcp-discovery-with-docker) covers DHCP, not full provisioning against these servers.

- [ ] **Environment:** Add `docker-tests/interop/` with Compose, selectable Watsen/Google servers and the production client binary. Pin tested versions and generate test credentials/configuration so runs need no manually prepared server.
- [ ] **Watsen:** Adapt [OPI's RESTCONF initialization](https://github.com/opiproject/sztp/blob/main/scripts/docker-entrypoint.sh). Its [package pin](https://github.com/opiproject/sztpd/blob/main/requirements.txt) is 0.0.15, while [PyPI](https://pypi.org/project/sztpd/) lists 0.0.46 as of 2026-10-28. Verify installation, configuration and package terms before pinning a test version. The old recipe does not prove current compatibility.
- [ ] **Google:** Run the client directly against [open-sztp](https://github.com/google/open-sztp). Probe endpoints independently so the first failure does not hide later differences: missing [host-meta route](https://github.com/google/open-sztp/blob/d2e4e3daecb3f66c84803270432f0aa553316b36/server.go), [serial-header lookup](https://github.com/google/open-sztp/blob/d2e4e3daecb3f66c84803270432f0aa553316b36/util/stub_dependency_provider.go), and [response fields/CMS SignedData](https://github.com/google/open-sztp/blob/d2e4e3daecb3f66c84803270432f0aa553316b36/handlers/bootstrap_data.go). Record incompatibilities without rewriting responses or claiming mTLS coverage from the default server.
- [ ] **Provisioning:** Where supported, check TLS, redirect, configuration bytes, pre/post script order, server acknowledgement and persisted completion. Test restart and recovery from a failed final report after completion is saved, without rerunning scripts. Exit status alone cannot prove these outcomes.
- [ ] **DHCP:** Reuse existing fixtures and retain the discovery catalog. Add one complete provisioning case per DHCP protocol to cover the gap between discovery and onboarding.
- [ ] **Automation:** Use the same runner locally and in a separate PR/scheduled/manual CI check, extending [planned CI](#ci-workflows-and-local-validation). Preserve failures, results, versions and client/server logs. Isolate runs and always clean containers, networks and state volumes to prevent interference. Leave branch protection unchanged.

Accept reproducible per-server outcomes with environment failures distinguished from interoperability failures. Document commands and demonstrated coverage without claiming full RFC compliance. Upstream source inspection is evidence for test targets, not proof of successful interoperability.

## Device-Specific Configuration and Boot Images

### Scope and Interfaces

Client responsibilities: transfer, hardware/OS detection, independent configuration/image handler selection, order and reporting. Compile separate Go interfaces. Authentication serials alone do not identify families.

Build interfaces/selection/reporting/tests before BF3 handlers/installers. Initial configuration must be verifiable/reversible without reboot. GRUB, firmware and image download/install/reboot need separate device designs. One-time onboarding only.

- Configuration handlers validate compatibility/settings, prepare `Commit`/`Rollback`/`Finalize` transactions and recover from persistent data.
- Image handlers check running images, verify/install replacements. Matching images need no install. Start with a test handler.
- Profiles describe OS/BFB versions, capabilities/differences. Platform and configuration-model versions remain separate.

Retain RFC envelope. Migrate free-form Base64 configuration to shared JSON containing model ID/version/settings. Models define ownership/deletion: `merge` combines within handler scope, `replace` replaces that scope, never generic JSON merge. Initially handlers perform all owned changes.

Handlers may use OPI/OVSDB/Linux tools. [OPI API](https://github.com/opiproject/opi-api)/[NVIDIA bridge](https://github.com/opiproject/opi-nvidia-bridge) are candidates, not complete coverage.

### Behavior and Lifecycle

Validate before changes. Select configuration/image support independently:

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

Transactions cover supported configuration only. Boot precedes scripts, commit precedes post script ([RFC 8572 §5.6](https://www.rfc-editor.org/rfc/rfc8572.html#section-5.6)).

### Completion and Recovery

- `config-complete` requires committed configuration. `boot-image-complete` requires a verified running image, not installer success before reboot.
- Unsupported operations emit `config-warning`/`boot-image-warning` with operation/device/reason, never success. Persist skips/reasons in journal/completion. Include them in `bootstrap-complete` across retries/restarts. Separate local outcome from acknowledgement.
- Otherwise successful onboarding with skips completes with warnings. Added support does not repeat provisioning without reset.
- This deliberately departs from RFC 8572's successful processing of all supplied criteria before `bootstrap-complete`. Preserve wire format, document departure, do not claim full compliance ([§5.6](https://www.rfc-editor.org/rfc/rfc8572.html#section-5.6), [§7.3](https://www.rfc-editor.org/rfc/rfc8572.html#section-7.3)).
- Journal in existing state directory. Restore handler scope on commit/post failure. Recover before retry. Stop changes on recovery failure. Journaled success with failed completion persistence must finish persistence/finalization without repeating scripts.
- Rollback covers handler-owned configuration only. Authors own script side effects/cleanup/reruns. Arbitrary shell commands/image replacement are outside guarantees.

### Acceptance

Test configuration/image handlers for:

- Every table case, configuration-only/image-only support and matching images.
- Scripts with missing content. Both skips plus successful scripts complete with warnings, never false success.
- Malformed/ambiguous input stopping before changes. Supported failures remaining errors.
- Lifecycle order, commit/post rollback and model-defined merge/replace boundaries.
- Interrupted recovery, stopping on recovery failure, completion-write recovery without repeated scripts.
- Durable skip reasons across recovery/retries/restarts. New support requires reset to reprovision.

Accept adding either capability without SZTP-layer changes. Real handlers/installers need hardware acceptance.

## Completed Work

- DHCP lab preparation: [automated DHCP tests](development.md#test-dhcp-discovery-with-docker).
- Bootstrap redirect address validation: [address validation](development.md#address-validation).
- HTTP request limits: [settings and defaults](usage.md#bound-http-requests) and [runtime behavior](development.md#http-request-limits).
