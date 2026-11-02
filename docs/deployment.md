<!--
SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
SPDX-License-Identifier: Apache-2.0
-->

# Deployment and maintenance

Install the client and manage saved state here. See the [quick start](../README.md#quick-start) for manual runs and [usage](usage.md) for settings and scripts.

- [Build a Linux binary](#build-a-linux-binary)
- [Install a systemd service](#install-a-systemd-service)
- [Understand completion reporting](#understand-completion-reporting)
- [Manage the state directory](#manage-the-state-directory)
- [Provision a device again](#provision-a-device-again)
- [Upgrade an installation](#upgrade-an-installation)

## Build a Linux binary

Build a static binary from the repository root.

For x86-64:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 \
  go build -mod=readonly -trimpath -ldflags="-s -w" \
  -o sztp-client-linux-amd64 .
```

For ARM64:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 GOARM64=v8.0 \
  go build -mod=readonly -trimpath -ldflags="-s -w" \
  -o sztp-client-linux-arm64 .
```

Copy it to the target host with the required [licenses and notices](development.md#prepare-a-distribution).

## Install a systemd service

The [example service](../packaging/systemd/sztp-client.service) runs as root for hardware access, DHCP and scripts. Run these steps on the target host from the repository directory.

### 1. Install the binary

```sh
sudo install -d -o root -g root -m 0755 /usr/local/bin
sudo install -o root -g root -m 0755 ./sztp-client /usr/local/bin/sztp-client
/usr/local/bin/sztp-client --help
```

For a static build, replace the source `./sztp-client` with `./sztp-client-linux-amd64` or `./sztp-client-linux-arm64`.

### 2. Install configuration and credentials

Prepare `config.yaml` from [the example](../config.yaml.example) with your server, serial and password. Use absolute credential paths:

```yaml
private-key: "/etc/sztp-client/certs/device_private_key.pem"
end-entity-cert: "/etc/sztp-client/certs/device_cert.pem"
trust-anchor-cert: "/etc/sztp-client/certs/cert_chain.pem"
```

Install your configuration and credentials, adjusting source filenames as needed:

```sh
sudo install -d -o root -g root -m 0700 /etc/sztp-client /etc/sztp-client/certs
sudo install -o root -g root -m 0600 ./config.yaml /etc/sztp-client/config.yaml
sudo install -o root -g root -m 0600 ./certs/device_private_key.pem /etc/sztp-client/certs/device_private_key.pem
sudo install -o root -g root -m 0600 ./certs/device_cert.pem /etc/sztp-client/certs/device_cert.pem
sudo install -o root -g root -m 0600 ./certs/cert_chain.pem /etc/sztp-client/certs/cert_chain.pem
```

### 3. Enable startup at boot

```sh
sudo install -o root -g root -m 0644 packaging/systemd/sztp-client.service /etc/systemd/system/sztp-client.service
sudo systemd-analyze verify /etc/systemd/system/sztp-client.service
sudo systemctl daemon-reload
sudo systemctl enable --now sztp-client.service
sudo journalctl -u sztp-client.service -b --no-pager
```

This starts provisioning immediately and enables boot starts. Systemd creates `/var/lib/sztp-client` and passes it as `--state-dir`; the client checks saved completion.

### Check whether provisioning finished

Check the journal and `/var/lib/sztp-client/onboarded.json`. A successful `systemctl start` means the binary started: `active (running)` can include provisioning or reporting retries. After a successful exit, `inactive (dead)` means it finished. “Already onboarded” means provisioning was skipped.

The service waits for network-online and the state mount. With `Type=exec`, ordered services can start once the binary executes, before provisioning finishes. Failed processes restart after 60 seconds. `TimeoutStartSec=0` disables the binary startup timeout.

## Understand completion reporting

After file preparation and both scripts succeed, the client:

1. Saves `onboarded.json` with report status `pending`.
2. Cleans the temporary workspace.
3. Retries `bootstrap-complete` until HTTP **204**.
4. Saves `acknowledged` and exits **0**.

Restarting with a pending report uses the saved endpoint and serial with current credentials, skipping discovery, bootstrap retrieval and scripts. Acknowledged starts skip reporting too. Both clean registered workspaces left by interrupted runs.

### When reporting fails

Reporting starts immediately, then waits **1, 2, 5, 10, 15, then 30 minutes** after failures; further waits stay at 30 minutes. Restarting restarts the sequence.

Progress requests default to a 30-second deadline and 64 KiB response limit. Set `progress-timeout` and `progress-max-response-bytes` through [HTTP settings](usage.md#bound-http-requests); pending reports use current settings. Other progress reports are best effort.

`SIGINT`/`SIGTERM` cancels final reporting or its wait, preserving pending state. Local credential or storage errors stop the process. Reports can repeat if acknowledgement was not saved; reporting failure alone never reruns scripts.

### When scripts can run again

Script failure or interruption before saving local completion can rerun earlier scripts. Make scripts tolerate reruns. Changing settings or serial numbers does not reset completion.

Boot-image handling and automatic configuration application remain [unimplemented](../README.md#current-limitations).

## Manage the state directory

Use persistent storage. Selection order is `--state-dir`, YAML `state-dir`, then `/var/lib/sztp-client`. Paths must be absolute; empty YAML uses the default, while an empty CLI value is an error. New directories use `0700`; existing permissions are preserved.

| File | Purpose |
| --- | --- |
| `onboarded.json` | Completion time, serial, report endpoint and status; permissions `0600`. |
| `.onboarding.lock` | Locks provisioning, retries and reset. Do not delete it. |
| `workspace.json` | Registers temporary workspaces for cleanup. |

Version **2** records retain the final endpoint after any redirect, without passwords or TLS keys. Version **1** counts as completed but cannot resume reporting.

Concurrent runs exit **1**; exit or crash releases the lock. Locking supports Linux and macOS; other platforms reject provisioning and reset.

### Handle state errors

Storage is checked before provisioning. Storage errors and unreadable, malformed or unknown-version records stop the client with exit **1**, without falling back to `/tmp`. A failed completion write stops without retrying scripts. Check logs and permissions; reset only to provision again.

### Store state on another mount

Create a persistent directory, such as `/mnt/persistent/sztp-client`, owned by the service user with permissions `0700`. Run `sudo systemctl edit sztp-client.service` and add:

```ini
[Unit]
RequiresMountsFor=
RequiresMountsFor=/mnt/persistent/sztp-client

[Service]
StateDirectory=
ExecStart=
ExecStart=/usr/local/bin/sztp-client --config=/etc/sztp-client/config.yaml --state-dir=/mnt/persistent/sztp-client
```

Then reload and restart:

```sh
sudo systemctl daemon-reload
sudo systemctl restart sztp-client.service
```

`StateDirectory=` accepts names below `/var/lib`, not absolute paths. The CLI path overrides YAML; use it for reset too.

## Provision a device again

For systemd:

```sh
sudo systemctl stop sztp-client.service
sudo /usr/local/bin/sztp-client --reset-onboarding --state-dir=/var/lib/sztp-client --config=""
sudo systemctl start sztp-client.service
```

For manual runs, stop the client first:

```sh
./sztp-client --reset-onboarding --state-dir="$HOME/.local/state/sztp-client" --config=""
```

Reset cleans the workspace, removes completion and pending reports, then exits without provisioning. Missing or malformed records are accepted. No credentials are needed; `--config=""` skips configuration loading. Start normally to reprovision. Reset cannot run concurrently or with discovery-only flags.

## Upgrade an installation

Wait for provisioning to finish. Install and verify the updated binary and service, then run `systemctl daemon-reload`. The next start uses them; keep completion state.

Devices without completion records provision on their first run. Review [DHCP selection](usage.md#find-the-server-through-dhcp) and [script paths](usage.md#write-provisioning-scripts) before upgrading older installations. Exclude completion records from reusable images and remove them during factory reset.
