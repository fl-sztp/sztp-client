# syntax=docker/dockerfile:1

# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 Anssi Söderena
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine3.24 AS builder
WORKDIR /src
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOFLAGS=-mod=readonly
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN mkdir /out && \
    go build -o /out/fl-sztp-client . && \
    go build -o /out/dhcp-fixture ./docker-setup/integration/fixture && \
    go test -c -o /out/control.test ./docker-setup/integration/internal/control && \
    go test -c -o /out/fixture.test ./docker-setup/integration/fixture && \
    go test -tags dhcp_integration -c -o /out/dhcp-integration.test ./docker-setup/integration

FROM alpine:3.24.2
RUN apk add --no-cache ca-certificates iproute2
COPY --from=builder /out/ /usr/local/bin/
COPY docker-setup/integration/entrypoint.sh /usr/local/bin/integration-entrypoint
RUN chmod 0755 /usr/local/bin/integration-entrypoint
ENTRYPOINT ["/usr/local/bin/integration-entrypoint"]
