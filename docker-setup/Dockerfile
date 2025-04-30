# Step 1: Build the Go application
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy only go.mod (ignore go.sum if it's missing)
COPY ../go.mod ./

# Download dependencies based on go.mod
RUN go mod download

# Copy the entire Go source code
COPY ../ ./

# Build the Go application
RUN go build -o fl-sztp-client .

# Step 2: Prepare Ubuntu image for running the Go application
FROM ubuntu:latest

WORKDIR /root
RUN apt-get clean
RUN apt-get update && apt-get install -y ca-certificates

COPY --from=builder /app/config.yaml /root/config.yaml
COPY --from=builder /app/certs /root/certs

COPY --from=builder /app/fl-sztp-client /usr/local/bin/fl-sztp-client