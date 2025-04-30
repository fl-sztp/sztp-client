# SZTP Client

Go implementation of a Secure Zero Touch Provisioning client, designed to automate the secure onboarding and provisioning of devices such as computers, DPUs, and network equipment. It communicates with an SZTP bootstrap server using mutual TLS authentication and supports secure device identity verification and configuration delivery.

Our future goal is to align with the [RFC 8572](https://datatracker.ietf.org/doc/html/rfc8572) specifications for SZTP.

This project is inspired by and derived from the OPI Project’s [sztp-agent](https://github.com/opiproject/sztp/tree/main/sztp-agent).

During the development of a production-ready SZTP client solution – tested on real hardware and in virtualized environments — we identified specific requirements that led us to design an independent implementation.  
We welcome the opportunity to contribute any improvements to the original project or to any future derivatives.

While our long-term goal is to make the client cross-platform, the current development and testing are targeted at Linux systems.

This client has been developed against and tested with [FusionLayer](https://www.fusionlayer.com) Xverse and Watsen Networks’ [SZTPD](https://watsen.net/products/sztpd). Other SZTP servers that support the RFC 8572 specifications should work as well, but we cannot guarantee this.

This project has been made possible by [FusionLayer Inc](https://www.fusionlayer.com).

## Requirements

- Linux (tested on Ubuntu; other distributions may work)
- Network connectivity to an SZTP bootstrap server
- Device certificates and keys (see configuration)

## Build

Clone the repository and build using Go (tested with Go 1.24):

```sh
git clone https://github.com/fl-sztp/sztp-client.git
cd sztp-client
go build -o sztp-client .
```

## Usage

Run the client with the desired options:

```sh
./sztp-client --config ./config.yaml
```

You can override configuration file values with command-line flags:

```sh
./sztp-client --server example.com:9090 --serial DEVICE_SERIAL --password DEVICE_PASSWORD --private-key ./key.pem --end-entity-cert ./cert.pem --trust-anchor-cert ./ca.pem
```

For all available flags, run:

```sh
./sztp-client --help
```

## Configuration

The client can be configured via a YAML file (default: `./config.yaml`). Example:

```yaml
server: "xverse.fusionlayer.com:9090"
insecure: false
debug: false
tmp: "/tmp"
serial: "device-serial-number"
password: "device-password"
private-key: "./certs/device_private_key.pem"
end-entity-cert: "./certs/device_cert.pem"
trust-anchor-cert: "./certs/cert_chain.pem"
```

- `server`: Address of the SZTP bootstrap server.
- `insecure`: Allow insecure TLS (skip certificate verification).
- `debug`: Enable debug logging.
- `tmp`: Temporary directory for intermediate files.
- `serial`: Device serial number.
- `password`: Device password.
- `private-key`: Path to the device's private key.
- `end-entity-cert`: Path to the device's certificate.
- `trust-anchor-cert`: Path to the trust anchor (CA) certificate.

Command-line flags take precedence over configuration file values.

## TODO

- Improve the client's retry logic when connecting to the bootstrap server.
- Implement a mechanism to detect if the client has already been onboarded.
- Support configuration for multiple bootstrap servers.
- Add comprehensive unit and integration tests.
- Implement support for boot image downloads and installation.
- Enhance DHCP-based bootstrap server discovery for non-Ubuntu operating systems.
- Extend support to additional operating systems (e.g., CentOS, Windows).
- Achieve full compliance with RFC 8572.

## Contributing

This project welcomes contributions and suggestions. Please submit issues and pull requests for improvements, bug fixes, or new features.

## License

This project is licensed under the Apache License 2.0.
