<!--
SPDX-FileCopyrightText: 2025 Anssi Söderena <https://github.com/asode>
SPDX-License-Identifier: Apache-2.0
-->

# SZTP Client

SZTP Client automates device onboarding using Secure Zero Touch Provisioning. It connects to a bootstrap server using mutual TLS, downloads configuration and runs provisioning scripts. It is written in Go and intended for computers, DPUs and network equipment.

The client has been tested with [FusionLayer Xverse](https://www.fusionlayer.com/xverse) and [Watsen Networks SZTPD](https://watsen.net/products/sztpd). Compatibility with other servers has not been verified. Full [RFC 8572](https://datatracker.ietf.org/doc/html/rfc8572) compliance is a work in progress.

The command-line application is currently the supported way to use this project. Its Go packages are implementation details and do not provide a stable public library API. Library support may be introduced in the future if there is a need for it.

This project has been made possible by [FusionLayer Inc](https://www.fusionlayer.com).

## Requirements

- Linux. Development and testing focus on Ubuntu.
- Go 1.27.0 or later to build from source.
- Network access to an SZTP bootstrap server.
- A device serial number, password, private key, device certificate and trust anchor certificate. The client can also read the serial number from firmware.

## Quick start

Run these steps on the target Linux device. For installation at boot, see [deployment](docs/deployment.md).

### 1. Build the client

```sh
git clone https://github.com/fl-sztp/sztp-client.git
cd sztp-client
go build -o sztp-client .
```

This creates the `sztp-client` binary in the repository directory.

### 2. Configure the device

```sh
cp config.yaml.example config.yaml
```

Edit `config.yaml` before running the client. Set your bootstrap server, device serial number and password. Place your device credentials in `certs/`, or change the three certificate and key paths to their actual locations. The example values are placeholders for your device.

Use an explicit `server` address for this first run. An empty `server` enables DHCP discovery, which normally needs root privileges. See the [configuration reference](docs/usage.md#configure-the-client) for all settings.

### 3. Run provisioning

This command starts provisioning and runs the scripts supplied by the server:

```sh
./sztp-client --config ./config.yaml --state-dir="$HOME/.local/state/sztp-client"
```

The state directory must be an absolute path. This example uses a persistent location writable by the current user. The default `/var/lib/sztp-client` normally requires administrator setup. The systemd service creates it automatically. Provisioning scripts may also need additional privileges.

Progress and script output appear on stderr. The client exits successfully after local provisioning completes and the server acknowledges the final report. Run `./sztp-client --help` to list command-line options.

## How completion works

The client records successful provisioning in its state directory. On subsequent runs, the client reads this record and skips provisioning. If the final report is still pending, the client retries reporting without rerunning scripts. Keep the state directory across restarts and upgrades.

If a run is interrupted before completion is saved, the client may run earlier scripts again. Write scripts so they can safely run more than once. Temporary files are removed after provisioning. Install anything you need to keep outside the workspace.

To provision a completed device again, follow the [reset procedure](docs/deployment.md#provision-a-device-again). Changing its configuration or serial number does not reset completion.

## Current limitations

Provisioning scripts must apply the downloaded configuration. The client currently ignores boot-image requests and `configuration-handling` modes such as `merge` and `replace`. These requests do not prevent it from recording completion. You can configure only one bootstrap server.

Linux is the current target. Other operating systems and full RFC 8572 behavior remain future work.

## Guides

| I want to… | Read |
| --- | --- |
| Change settings, test DHCP or write provisioning scripts | [Using the client](docs/usage.md) |
| Install with systemd, inspect state or reprovision | [Deployment and maintenance](docs/deployment.md) |
| Run tests or prepare a distribution | [Development](docs/development.md) |

## Contributing and credits

Issues and pull requests are welcome. See the [development guide](docs/development.md) for checks to run with a change.

This project is derived from the OPI Project’s [sztp-agent](https://github.com/opiproject/sztp/tree/main/sztp-agent) and made possible by [FusionLayer Inc](https://www.fusionlayer.com).

## License

This project is licensed under the [Apache License 2.0](LICENSE). When distributing binaries or images, also include the applicable dependency licenses and notices. See [preparing a distribution](docs/development.md#prepare-a-distribution).
