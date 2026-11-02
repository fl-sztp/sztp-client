# syntax=docker/dockerfile:1

# SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
# SPDX-License-Identifier: Apache-2.0

FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine3.24 AS builder
WORKDIR /src
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOFLAGS=-mod=readonly
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN mkdir /out && \
    go build -o /out/fl-sztp-client . && \
    go build -o /out/dhcp-fixture ./docker-tests/integration/fixture && \
    go test -c -o /out/client.test . && \
    go test -c -o /out/device.test ./sztpclient && \
    go test -c -o /out/dhcp.test ./dhcpclient && \
    go test -c -o /out/serveraddr.test ./serveraddr && \
    go test -c -o /out/control.test ./docker-tests/integration/internal/control && \
    go test -c -o /out/fixture.test ./docker-tests/integration/fixture && \
    go test -tags dhcp_integration -c -o /out/dhcp-integration.test ./docker-tests/integration
RUN sh scripts/collect-licenses.sh /licenses -test -tags=dhcp_integration \
    . ./sztpclient ./dhcpclient ./serveraddr \
    ./docker-tests/integration/internal/control \
    ./docker-tests/integration/fixture ./docker-tests/integration

FROM alpine:3.24.2
RUN apk add --no-cache ca-certificates iproute2
COPY --from=builder /out/ /usr/local/bin/
COPY --from=builder /licenses/ /usr/share/licenses/sztp-client/
COPY docker-tests/integration/entrypoint.sh /usr/local/bin/integration-entrypoint
RUN chmod 0755 /usr/local/bin/integration-entrypoint
ENTRYPOINT ["/usr/local/bin/integration-entrypoint"]
