//go:build linux

/*
SPDX-License-Identifier: Apache-2.0
Copyright (C) 2026 Anssi Söderena

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/server4"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/dhcpv6/server6"
)

func main() {
	var err error
	if len(os.Args) == 2 && os.Args[1] == "--healthcheck" {
		err = healthcheck()
	} else if len(os.Args) != 1 {
		err = fmt.Errorf("usage: fixture [--healthcheck]")
	} else {
		err = run()
	}
	if err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func healthcheck() error {
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://127.0.0.1:8081/health")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("fixture health returned HTTP %d", response.StatusCode)
	}
	return nil
}

func run() error {
	iface, err := net.InterfaceByName("eth0")
	if err != nil {
		return err
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return err
	}
	var serverIP net.IP
	for _, addr := range addrs {
		if ip, ok := addr.(*net.IPNet); ok && ip.IP.To4() != nil {
			serverIP = ip.IP.To4()
			break
		}
	}
	if serverIP == nil {
		return fmt.Errorf("eth0 has no IPv4 address for the server identifier")
	}
	f := newFixture(serverIP, iface.HardwareAddr)
	v4, err := server4.NewServer("eth0", nil, f.handle4, server4.WithLogger(&servingLogger4{f}))
	if err != nil {
		return fmt.Errorf("bind DHCPv4: %w", err)
	}
	defer v4.Close()
	v6, err := server6.NewServer("eth0", nil, f.handle6, server6.WithLogger(&servingLogger6{f}))
	if err != nil {
		return fmt.Errorf("bind DHCPv6: %w", err)
	}
	defer v6.Close()
	listener, err := net.Listen("tcp", ":8081")
	if err != nil {
		return err
	}
	httpServer := &http.Server{Handler: f.routes(), ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	defer httpServer.Close()
	errors := make(chan error, 3)
	go func() { err := v4.Serve(); f.setServing(4, false); errors <- fmt.Errorf("DHCPv4 stopped: %w", err) }()
	go func() { err := v6.Serve(); f.setServing(6, false); errors <- fmt.Errorf("DHCPv6 stopped: %w", err) }()
	go func() { errors <- httpServer.Serve(listener) }()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	select {
	case <-ctx.Done():
		return nil
	case err := <-errors:
		return err
	}
}

// The pinned server packages emit this message inside Serve after binding and
// immediately before their receive loops. Use it to expose socket readiness.
type servingLogger4 struct{ f *fixture }

func (*servingLogger4) PrintMessage(string, *dhcpv4.DHCPv4) {}
func (l *servingLogger4) Printf(format string, args ...interface{}) {
	if format == "Ready to handle requests" {
		l.f.setServing(4, true)
		log.Print("DHCPv4 serving on eth0 UDP 67")
	}
}

type servingLogger6 struct{ f *fixture }

func (*servingLogger6) PrintMessage(string, *dhcpv6.Message) {}
func (l *servingLogger6) Printf(format string, args ...interface{}) {
	if format == "Ready to handle requests" {
		l.f.setServing(6, true)
		log.Print("DHCPv6 serving on eth0 UDP 547")
	}
}
