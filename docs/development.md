<!--
SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
SPDX-License-Identifier: Apache-2.0
-->

# Development

Use this guide to test changes or package releases. For building and running, see the [quick start](../README.md#quick-start) and [deployment guide](deployment.md).

- [Run unit tests and static checks](#run-unit-tests-and-static-checks)
- [Check GitHub Actions workflows before committing](#check-github-actions-workflows-before-committing)
- [Test DHCP discovery with Docker](#test-dhcp-discovery-with-docker)
- [Protocol details](#protocol-details)
- [Runtime details](#runtime-details)
- [Prepare a distribution](#prepare-a-distribution)

## Run unit tests and static checks

The module requires Go **1.27.0 or later**. The Docker test builder pins **1.27.1**. Release builds may use newer patched compilers. Target hosts need no Go installation. Lowering the minimum requires explicit rejection of malformed UTF-8 in hostnames and compatibility verification, tracked in [Go 1.26 compatibility](PLAN.md#go-126-compatibility).

On Linux, from the repository root:

```sh
go test -mod=readonly ./...
go vet -mod=readonly ./...
go test -mod=readonly -race ./...
```

Run as an ordinary user so permission tests do not skip. Race tests need CGO and a C compiler. Real DHCP packet tests run separately in Docker.

## Check GitHub Actions workflows before committing

The root `.pre-commit-config.yaml` pins actionlint and REUSE. Both hooks run on
every commit and check the whole repository rather than only changed files.
Actionlint checks workflow syntax, expressions and job dependencies. Install
ShellCheck too so it checks Bash and sh commands in `run:` steps.

On macOS with Homebrew:

```sh
brew install pre-commit shellcheck
```

On Debian or Ubuntu, with Python 3 and venv support installed:

```sh
sudo apt-get install --yes pipx shellcheck
pipx install pre-commit==4.7.0
pipx ensurepath
```

Open a new terminal after updating PATH. Pre-commit needs Python 3. The actionlint
hook uses Go in an isolated environment and can download Go when it is absent.
The first run needs network access to install the pinned hooks and dependencies.
Docker is unnecessary for these hooks.

From the repository root, install the hook once per clone:

```sh
pre-commit install
```

Run all checks manually, or select one:

```sh
pre-commit validate-config
pre-commit run --all-files
pre-commit run actionlint --all-files
pre-commit run reuse --all-files
```

An unsuccessful hook prevents the commit. During a commit, pre-commit temporarily
hides unstaged edits to tracked files. Manual runs check the working tree.
Whole-repository tools can also see nonignored untracked files, so add intended
new files before checking. A bypassed or uninstalled hook does not provide local
verification. CI runs actionlint independently in `workflow-lint.yml`, while
`reuse.yml` retains its separate licensing check.

For an optional local execution test with act, start Docker and choose a runner
image with Python, Go and sudo:

```sh
act pull_request -W .github/workflows/workflow-lint.yml -j actionlint
```

On Apple Silicon, prefer an ARM64-compatible runner image. Use
`--container-architecture linux/arm64` to select ARM64 explicitly.
Use `--container-architecture linux/amd64` only when the runner image or
an action requires AMD64, which requires emulation on ARM64.
This setting applies to act independently of `SZTP_DHCP_PLATFORM`.
Local runs do not reproduce all GitHub behavior. See
[act documentation](https://nektosact.com/usage/index.html) for setup and limitations.

See [actionlint usage](https://github.com/rhysd/actionlint/blob/main/docs/usage.md)
and [pre-commit](https://pre-commit.com/).

## Test DHCP discovery with Docker

Docker is used only for testing. The automated environment lives in `docker-tests/integration/`. Install the client on target devices as a [Linux binary or systemd service](deployment.md).

Use Bash and Docker Desktop or rootful Linux Docker Engine with:

- BuildKit and Compose **2.27.1 or later** for [service network `driver_opts`](https://docs.docker.com/reference/compose-file/services/#driver_opts).
- `com.docker.network.endpoint.sysctls` support.
- A Linux/arm64 or Linux/amd64 Docker server. The runner selects its native architecture by default.

Host Go and device certificates are unnecessary. From the repository root, run all 89 cases:

```sh
bash docker-tests/integration/run.sh
```

To select a target explicitly, set `SZTP_DHCP_PLATFORM` to `linux/arm64` or
`linux/amd64`:

```sh
SZTP_DHCP_PLATFORM=linux/amd64 bash docker-tests/integration/run.sh
```

The default follows the Docker server, including remote Docker contexts.
Cross-architecture execution requires emulation. Validate each architecture on
native hardware when possible. An ARM64 pass does not verify AMD64 execution.
The runner checks the image platform and the tests check that the client ELF
architecture matches the test process. Unsupported targets are rejected.

Quick mode runs 85, omitting four timeout/retry checks:

```sh
bash docker-tests/integration/run.sh --quick
```

Select a case by its exact catalog name:

```sh
bash docker-tests/integration/run.sh --case v4_rfc_single
```

`--quick` accepts cases such as `v6_text_hostname`, but excludes timing cases such as `silent_v6_timeout_then_retry`. See `--help`. Client, device-information, DHCP, address-validation, fixture and control unit tests run in every mode.

### Read results and investigate failures

The runner prints its exit status and artifact directory containing client logs, packet events, and build/test/cleanup logs. Allow about 200 seconds after building: the suite includes a 30-second progress deadline, 35-second DHCP timeouts and a 60-second retry.

Containers and networks are removed on success, failure or interrupt/termination signals. Images, cache and artifacts remain.

### What the suite covers

The production Linux/arm64 or Linux/amd64 client exchanges real DHCP packets on a dual-stack network. Coverage includes options 143/136, text and URI lists, Unicode, long URIs, invalid addresses, malformed payloads, IPv4 fragments, protocol selection, fallback/retries, diagnostic reports, early stopping, configuration precedence, identity and certificate inspection. Full provisioning and BF3 hardware tests are separate.

Each run has its own Compose project and bridge: no NAT, default routes, IPv6 router advertisements or published ports. `internal: true` drops required raw IPv4 broadcasts, so isolation uses disabled NAT and removed routes.

### NetworkManager coverage

Coexistence remains unverified: Docker tests do not run NetworkManager. The [planned Fedora test](PLAN.md#networkmanager-compatibility) covers SZTP discovery, address acquisition and lease renewal. No procedure or results exist yet.

## Protocol details

Conveyed information requires exactly one non-null `onboarding-information` or `redirect-information` envelope. Duplicate envelope members, including repeated names, are rejected. Empty onboarding objects are valid. Missing or malformed envelopes cannot complete onboarding. One bootstrap redirect is followed. Its response must contain onboarding information before scripts run.

### Address validation

See [accepted addresses and server selection](usage.md#choose-a-bootstrap-server). Hostnames allow letters, digits, hyphens and underscores. Labels must be nonempty, at most 63 bytes and without edge hyphens. Names allow 253 bytes excluding a trailing root dot. Normalization preserves ASCII spelling, port spelling and the root dot.

Redirects use the same HTTP-to-HTTPS and Unicode normalization. A redirect `address` also accepts bare IPv6. Its separate `port` (1..65535) replaces a valid embedded port. Invalid input is rejected before changing servers or requesting the destination.

### DHCP options

`dhcpclient.GetServerFromDHCP4` and `dhcpclient.GetServerFromDHCP6` return normalized HTTPS origins or an empty string.

DHCPv4 option **143** and DHCPv6 option **136** accept text or RFC 8572 URI lists. Each URI starts with its two-byte unsigned big-endian byte length, excluding that prefix: `https://bootstrap:8080` is `00 16` followed by 22 bytes.

Validate complete framing, then choose the first usable address, skipping empty/invalid entries. Malformed lists fall back to text parsing. Unusable options are ignored. Reassemble DHCPv4 fragments first.

### DHCP sockets

Linux queries use `github.com/insomniacslk/dhcp`, independently of NetworkManager lease data, D-Bus or dispatcher environment.

- DHCPv4 uses `AF_PACKET`, including UDP port 68 traffic. Another client's ordinary UDP listener alone does not block it.
- DHCPv6 binds the interface's link-local IPv6 address on UDP port 546. Existing listeners can conflict.

Queries use `context.Background()` and waits use `time.Sleep`, delaying cancellation. After ten unsuccessful rounds, bootstrap uses its hardcoded fallback without further DHCP attempts. See [planned corrections](PLAN.md#dhcp-retry-and-cancellation).

## Runtime details

### HTTP request limits

Explicit CLI overrides precede YAML for [six HTTP settings](usage.md#bound-http-requests). Explicit zero is rejected. Resolved `sztpclient.HTTPRequestLimits` reach bootstrap and resumed reporting through `Client.RequestLimits`, without persisting in state.

Configure limits before requests. API zero fields use `DefaultHTTPRequestLimits()`. `Validate()` rejects negative/unrepresentable values. Each request gets a child context and reads at most the body limit plus one byte. Oversized responses return `ErrResponseTooLarge` without content. Wrapping preserves `errors.Is` for that sentinel, `context.DeadlineExceeded` and `context.Canceled`.

Tests cover absent/earlier caller deadlines, cancellation, fixed/chunked/gzip boundaries, bounded reads, bootstrap recovery and resumed reports. Error redaction and diagnostic limits remain [planned](PLAN.md#bootstrap-error-diagnostics).

### Workspaces

Private children of `tmp` use `0700`. Missing parents use `0755`. Empty `tmp` uses Linux's `$TMPDIR` when set, otherwise `/tmp`. Existing permissions are preserved. A failed parent write probe or workspace creation triggers a logged fallback to the system temporary directory. Failure there stops startup.

Before writing files, `workspace.json` records the canonical absolute path. Onboarding/reset cleans registered workspaces under the state lock. Cleanup failure stops provisioning and retains registration. Saved completion stays valid.

The entire workspace, including downloads/logs, is removed after saving completion and before final reporting. Failed attempts are cleaned before retrying. No previous attempt is retained, and there is no workspace size cap. Parents, unrelated files and older unregistered directories are untouched. See [state](deployment.md#manage-the-state-directory), [reset](deployment.md#provision-a-device-again) and [script guidance](usage.md#write-provisioning-scripts).

### Logging

Go `log/slog` text records go to stderr. Discovery reports use stdout. Each line has `time`, uppercase `level`, `msg` and applicable fields, without terminal colours. Default `INFO`. `--debug` enables `DEBUG`. Values containing spaces or special characters are quoted. Decode them before comparison. Update old Logrus parsers.

Variable fields include `server`, `interface`, `option`, `protocol`, `path`, `phase`, `stream`, `attempt`, `delay` and `error`. Match `event` and fields rather than message text:

| Event | Fields |
| --- | --- |
| `bootstrap_server_selected` | `server`: selected bootstrap address. |
| `dhcp_retry_scheduled` | `attempt`: failed discovery round. `delay`: wait before the next round. |

```text
time=2026-10-21T12:00:01.000Z level=WARN msg="Retrying DHCP discovery" event=dhcp_retry_scheduled attempt=1 delay=1m0s
```

Hardware logs add `component=ghw`. Application settings override `GHW_LOG_LEVEL`, `GHW_LOG_LOGFMT` and `GHW_DISABLE_WARNINGS`. Other hardware options, including `GHW_CHROOT` and `GHW_DISABLE_TOOLS`, retain their behavior.

Retryable bootstrap/reporting failures use `WARN` with `error`. Terminal startup/onboarding failures use `ERROR`. Handling callers log returned HTTP/script errors once. See [script logs](usage.md#read-script-logs) for streams and report limits.

## Prepare a distribution

### Build and runtime requirements

Source packages need project/dependency sources and build instructions. RPM `BuildRequires` includes Go and build tools. `Requires` covers installed-program dependencies. Binary installation needs no Go compiler. Match target OS/architecture and include any shared-library dependencies. The Docker test builder uses `CGO_ENABLED=0`.

### Installed files and service integration

- Install the binary, licenses and notices.
- Install the man page when [implemented](PLAN.md#command-line-manual-page): RPM path `%{_mandir}/man1/`, mode `0644`, file entry `%{_mandir}/man1/sztp-client.1*`.
- Match the [service's](../packaging/systemd/sztp-client.service) `ExecStart` to the installed binary: the example uses `/usr/local/bin/sztp-client`, the Fedora review spec `/usr/bin/sztp-client`.

The service runs as root and reads the state path from its YAML configuration. The client creates the persistent state directory when needed. Include the [deployment guide](deployment.md#install-a-systemd-service) as package documentation for credentials, startup and logs.

### Licenses and notices

Distribute the [Apache license](../LICENSE) and applicable third-party licenses, copyright notices and NOTICE attributions for linked dependencies.

The Docker test build collects unchanged dependency and Go toolchain notices under `/usr/share/licenses/sztp-client/`. `modules.txt` lists dependency versions. The test image includes test-binary notices.

For standalone ARM64 binaries:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  sh scripts/collect-licenses.sh /tmp/sztp-client-licenses .
```

Distribute the collected directory with the binary. Match `GOOS`, `GOARCH`, `CGO_ENABLED`, build tags and package selection. Collection is conservative. Review compatibility and base-image/OS-package licenses separately.
