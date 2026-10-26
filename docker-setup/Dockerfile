# SPDX-FileCopyrightText: 2025 Anssi Söderena <https://github.com/asode>
# SPDX-License-Identifier: Apache-2.0

# Step 1: Build the Go application
# Modified for sztp-client.
FROM golang:1.27.1-alpine3.24 AS builder

WORKDIR /app
ENV CGO_ENABLED=0 GOFLAGS=-mod=readonly

# Copy dependency manifests before the source to reuse the download cache.
COPY go.mod go.sum ./

# Download dependencies based on go.mod
RUN go mod download

# Copy the entire Go source code
COPY . .

# Build the Go application
RUN go build -o fl-sztp-client .
RUN sh docker-setup/collect-licenses.sh /licenses .

# Step 2: Prepare Ubuntu image for running the Go application
FROM ubuntu:latest

WORKDIR /root
RUN apt-get clean
RUN apt-get update && apt-get install -y ca-certificates

COPY --from=builder /app/fl-sztp-client /usr/local/bin/fl-sztp-client
COPY --from=builder /licenses/ /usr/share/licenses/sztp-client/
