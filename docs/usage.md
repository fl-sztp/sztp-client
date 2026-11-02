<!--
SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
SPDX-License-Identifier: Apache-2.0
-->

# Using the client

See [quick start](../README.md#quick-start) for first runs and [deployment](deployment.md) for installation/state.

- [Configure the client](#configure-the-client)
- [Choose a bootstrap server](#choose-a-bootstrap-server)
- [Test DHCP discovery](#test-dhcp-discovery)
- [Write provisioning scripts](#write-provisioning-scripts)
- [Read logs](#read-logs)

## Configure the client

Copy [config.yaml.example](../config.yaml.example) to `config.yaml` and replace its values. Without `--config`, the client uses `/etc/sztp-client/config.yaml` if present, otherwise `./config.yaml`. Select only one file with `--config PATH`, or disable all file loading with `--config ""`. Files are not merged. An unreadable, malformed or invalid selected file stops startup without falling back.

Each setting has a matching flag. Requirements apply to normal provisioning:

| Setting | Value or default |
| --- | --- |
| `server` | Bootstrap address. An empty value enables DHCP. |
| `serial` | Authentication serial, read from firmware if omitted. |
| `password` | Required device password. |
| `private-key` | Required path to the device's private key. |
| `end-entity-cert` | Required path to the device's certificate. |
| `trust-anchor-cert` | Required path to the CA certificate or trust bundle. |
| `insecure` | Skip server certificate verification (default `false`). |
| `debug` | Enable debug logs (default `false`). |
| `tmp` | Parent for temporary workspaces. An empty value uses the system temporary directory. |
| `state-dir` | Absolute persistent-state path (default `/var/lib/sztp-client`). |

Nonempty string flags override YAML; empty strings normally preserve it. `--debug`/`--insecure` enable settings but cannot disable YAML `true`. Exceptions: [state-dir](deployment.md#manage-the-state-directory) and [HTTP limits](#bound-http-requests).

Explicit config files must exist and contain valid YAML. Both default files may be absent for diagnostics, reset and acknowledged/version 1 completion. [Pending reports](deployment.md#understand-completion-reporting) need credentials.

Replace placeholders:

```sh
./sztp-client --config "" \
  --state-dir="$HOME/.local/state/sztp-client" \
  --server bootstrap.example:9090 \
  --serial DEVICE_SERIAL --password DEVICE_PASSWORD \
  --private-key ./certs/device_private_key.pem \
  --end-entity-cert ./certs/device_cert.pem \
  --trust-anchor-cert ./certs/cert_chain.pem
```

Credential paths are relative to the current directory; use absolute paths for services.

### Bound HTTP requests

YAML or matching flags:

| Request | Deadline setting and default | Response-size setting and default |
| --- | --- | --- |
| Host metadata | `host-meta-timeout: 30s` | `host-meta-max-response-bytes: 1048576` (1 MiB) |
| Bootstrap data, including a redirected bootstrap server | `bootstrap-timeout: 120s` | `bootstrap-max-response-bytes: 16777216` (16 MiB) |
| Progress and completion reports | `progress-timeout: 30s` | `progress-max-response-bytes: 65536` (64 KiB) |

Use positive durations (`30s`, `2m`) and integer byte counts. Omitted/YAML `null` uses defaults. Explicit CLI values override even invalid YAML scalars before validation. Empty, zero, negative, malformed or overflowing values stop startup before requests. Unlimited values are unsupported.

Example:

```sh
./sztp-client --config ./config.yaml --bootstrap-timeout 3m --bootstrap-max-response-bytes 33554432
```

Deadlines cover connection, TLS, redirects and response reading, not whole onboarding, DHCP or scripts. Timeout/oversize fails the attempt; bootstrap retries after one minute. Error reporting uses its own progress deadline; pending reports follow the [retry schedule](deployment.md#when-reporting-fails).

Count decompressed body bytes regardless of `Content-Length`. Exact-limit responses pass; larger responses fail without reading the remainder. Bootstrap includes JSON/Base64/CMS envelopes; progress limits server responses, not outgoing reports or script stdout. Larger limits permit greater resource use.

Pending reports resume with current settings; changes never repeat completed provisioning.

## Choose a bootstrap server

Accepts hostnames, `host:port`, HTTP/HTTPS URLs and bracketed IPv6 such as `[2001:db8::1]:9090`. HTTPS is always used, with the supplied port or 443. Userinfo/path/query/fragment are removed: `http://bootstrap:8080/restconf` becomes `https://bootstrap:8080`.

Host metadata, bootstrap data, progress and completion reports require HTTPS for the initial request and every HTTP redirect. Redirects to HTTP are rejected before sending a request to the target, including with `insecure: true`. That setting only skips server certificate verification. HTTPS redirects remain supported, with at most nine redirects per exchange. These transport rules follow [RFC 8040 Section 2.1](https://www.rfc-editor.org/rfc/rfc8040.html#section-2.1) and [RFC 6415 Section 5](https://www.rfc-editor.org/rfc/rfc6415.html#section-5).

Library callers must supply HTTPS URLs. Unlike configuration input, direct HTTP URLs passed to `sztpclient.Client` are rejected when making a request rather than upgraded to HTTPS.

Validation is local; DNS occurs on connection. Unicode becomes IDNA ASCII: `bücher.example` becomes `xn--bcher-kva.example`. See [validation rules](development.md#address-validation). Invalid addresses stop new provisioning/diagnostics; saved completion and reset need none.

### Find the server through DHCP

Set `server: ""`:

1. Try DHCPv4, then immediately try DHCPv6 if no usable server was found.
2. Repeat for up to 10 rounds, waiting one minute between failed rounds.
3. After all rounds fail, immediately use `https://xverse.fusionlayer.com:9090`.

IPv6-only networks wait for IPv4. Older versions preferred IPv6; check differing advertisements before upgrading or choose a fixed server.

### Configure DHCP options

DHCPv4 option **143** and DHCPv6 option **136** accept text or RFC 8572 URI lists; see [encoding rules and example](development.md#dhcp-options).

## Test DHCP discovery

On Linux, inspect DHCP, identity and certificates without provisioning:

```sh
sudo ./sztp-client --discover-only
```

No password/certificates required. Select exactly one mode:

| Flag | Probes |
| --- | --- |
| `--discover-only` | DHCPv4, then DHCPv6. |
| `--discover-only-dhcp4` | DHCPv4 only. |
| `--discover-only-dhcp6` | DHCPv6 only. |

CLI-only discovery flags exclude `--reset-onboarding`. `--discover-stop-on-success` requires discovery mode and stops after an accepted DHCP address. Identity/certificate checks still finish despite missing authentication serials.

Skip config loading and set identity (`--debug` adds detail):

```sh
sudo ./sztp-client --discover-only --config "" --serial DEVICE_SERIAL
```

### Read the result

Reports use stdout; progress/diagnostics use stderr.

| Report section | Contents |
| --- | --- |
| DHCP results | Accepted HTTPS addresses and reasons for skipped protocols. Logs explain missing options, rejected values, socket errors and timeouts. |
| Server selection | Address and source, preferring CLI/config, then DHCPv4, then DHCPv6. |
| Device identity | Hardware serial and firmware source, authentication serial and source, model, OS and version. |
| Certificates | Paths, subjects, issuers, validity, SHA-256 fingerprints and whether the certificate and private key load as a matching pair. |

`serial` overrides authentication identity only; hardware serials remain separate. Firmware `0`/`unknown` means missing. Inspection covers chains/trust bundles; credential errors do not stop DHCP. Reports include `insecure`, never passwords/private keys.

| Exit status | Meaning |
| --- | --- |
| **0** | DHCP returned an accepted address **and** an authentication serial is available. |
| **1** | Either requirement is missing. |

Configured servers cannot replace DHCP success; optional model/OS/certificate details do not affect status.

### What the test covers

One round per selected protocol, stopping at its first usable server; interfaces, validation, retransmissions and timeouts match normal discovery. Silent servers take about 35 seconds per interface/protocol. Normally requires root/equivalent socket privileges.

Configured servers neither disable probes nor trigger early stopping. Reports explain normal IPv4 priority when unselected, and retries/fallback when no server is selected. Diagnostics perform no extra rounds/fallback.

Checks do not verify DNS, HTTPS, server trust or authentication. They send no DHCP REQUEST, install no IP configuration, run no scripts and neither read nor modify onboarding state/workspaces.

## Write provisioning scripts

Private attempt directory:

```text
<tmp>/sztp-client-<UTC timestamp>-<random suffix>/
└── onboarding-<UTC timestamp>-<random suffix>/
    ├── pre-configuration.sh
    ├── configuration
    └── post-configuration.sh
```

Absolute path logged. All three Base64 fields are decoded, written and closed before execution; missing/empty fields create empty files. Preparation failures stop attempts. Scripts use `0755`, configuration `0744`.

`pre-configuration.sh` must succeed before `post-configuration.sh`. Both start in the attempt directory using Bash when available, otherwise `/bin/sh`. Read `./configuration` and share relative files; each script's `cd` affects only itself.

Scripts apply configuration: `configuration-handling` modes `merge`/`replace` remain unimplemented. `config-complete` currently confirms file preparation before scripts.

Tolerate failure/interruption reruns; see [completion reporting](deployment.md#understand-completion-reporting). Migrate older timestamp-prefixed filenames to the fixed names above; do not assume the client's launch directory.

### Keep files that must survive provisioning

Keep persistent files/logs outside the workspace, which is removed after saving completion, before final-report retries. Failed attempts are cleaned before retry.

Next provisioning/reset cleans registered interrupted workspaces; cleanup errors stop provisioning. See [workspace lifecycle](development.md#workspaces) and [reset](deployment.md#provision-a-device-again).

## Read logs

Logs use stderr; discovery uses stdout. Default `INFO`; `--debug` enables `DEBUG`. Records have timestamp, level and message; `event` is optional:

```text
time=2026-10-21T12:00:00.000Z level=INFO msg="Using server address" event=bootstrap_server_selected server=https://bootstrap:8080
```

Retryable bootstrap/reporting failures use `WARN`; terminal startup/onboarding failures use `ERROR`. See [service logs](deployment.md#install-a-systemd-service). Update Logrus parsers; match `event`/fields rather than messages ([field reference](development.md#logging)).

### Read script logs

Script stdout/stderr logs have normal level and phase/stream labels. Systemd retains them after cleanup.

Informational reports include only the last **64 KiB of script stdout**, marking truncation. Local logs/downloads are uncapped by this limit.
