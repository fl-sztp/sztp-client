// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fl-sztp-client/sztpclient"
)

func certificateFixture(t *testing.T, name string, start, end time.Time) ([]byte, []byte, []byte) {
	t.Helper()
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(42), Subject: pkix.Name{CommonName: name}, NotBefore: start, NotAfter: end, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, template, template, pub, private)
	if err != nil {
		t.Fatal(err)
	}
	key, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), der
}

func writeCertificateFixture(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCertificateReport(t *testing.T) {
	now := time.Now()
	leaf, key, der := certificateFixture(t, "device", now.Add(-time.Hour), now.Add(time.Hour))
	root, wrongKey, _ := certificateFixture(t, "root", now.Add(-time.Hour), now.Add(time.Hour))
	expired, _, _ := certificateFixture(t, "expired", now.Add(-2*time.Hour), now.Add(-time.Hour))
	future, _, _ := certificateFixture(t, "future", now.Add(time.Hour), now.Add(2*time.Hour))
	for _, tc := range []struct {
		name             string
		cert, key, trust []byte
		insecure         bool
		want             []string
	}{
		{name: "leaf chain and trust bundle", cert: append(append([]byte(nil), leaf...), root...), key: key, trust: append(append([]byte(nil), root...), leaf...), want: []string{`Client certificate 1:`, `Client certificate 2:`, `Subject: "CN=device"`, `Issuer: "CN=device"`, fmt.Sprintf("SHA-256: %x", sha256.Sum256(der)), `Trust anchor 2:`, `key matches certificate`, `within validity period`, `verification: enabled`}},
		{name: "wrong private key", cert: leaf, key: wrongKey, trust: root, want: []string{`cannot be loaded:`, `private key does not match public key`}},
		{name: "validity and insecure", cert: append(append([]byte(nil), expired...), future...), key: key, trust: root, insecure: true, want: []string{`(expired)`, `(not yet valid)`, `trust anchors are not enforced`}},
		{name: "malformed files", cert: []byte("not a certificate"), key: []byte("not a key"), trust: []byte("not a CA"), want: []string{`no readable PEM certificates found`, `cannot be loaded`}},
		{name: "not configured", want: []string{`not configured completely`, `Trust anchor file: not found / not configured`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := &Config{AllowInsecure: tc.insecure}
			dir := t.TempDir()
			if tc.cert != nil {
				config.DeviceEndEntityCert = writeCertificateFixture(t, dir, "cert.pem", tc.cert)
			}
			if tc.key != nil {
				config.DevicePrivateKey = writeCertificateFixture(t, dir, "key.pem", tc.key)
			}
			if tc.trust != nil {
				config.BootstrapTrustAnchor = writeCertificateFixture(t, dir, "ca.pem", tc.trust)
			}
			var out bytes.Buffer
			probe := func(*slog.Logger) (string, error) { return "https://server.example", nil }
			code := runDiscoveryReport(&out, discoveryTestLogger(), config,
				func(*slog.Logger, string) (sztpclient.DeviceInformation, error) {
					return sztpclient.DeviceInformation{SerialNumber: "serial"}, nil
				}, probe, probe)
			if code != 0 {
				t.Fatalf("optional certificate inspection changed exit code to %d", code)
			}
			for _, want := range tc.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("missing %q:\n%s", want, out.String())
				}
			}
			if strings.Contains(out.String(), "-----BEGIN PRIVATE KEY-----") || (len(tc.key) > 0 && strings.Contains(out.String(), string(tc.key))) {
				t.Fatal("private key material leaked")
			}
		})
	}
}

func TestReadCertificates(t *testing.T) {
	now := time.Now()
	cert, key, _ := certificateFixture(t, "device", now.Add(-time.Hour), now.Add(time.Hour))
	bad := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("invalid DER")})
	for _, tc := range []struct {
		name    string
		data    []byte
		count   int
		wantErr bool
	}{
		{"missing", nil, 0, true},
		{"empty", []byte{}, 0, true},
		{"only key", key, 0, true},
		{"key ignored", append(append([]byte(nil), key...), cert...), 1, false},
		{"truncated PEM", []byte("-----BEGIN CERTIFICATE-----\nAA=="), 0, true},
		{"malformed DER", bad, 0, true},
		{"partially valid bundle", append(append([]byte(nil), cert...), bad...), 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bundle.pem")
			if tc.data != nil {
				if err := os.WriteFile(path, tc.data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			certs, err := readCertificates(path)
			if len(certs) != tc.count || (err != nil) != tc.wantErr {
				t.Fatalf("count=%d error=%v, want %d/%v", len(certs), err, tc.count, tc.wantErr)
			}
		})
	}
}
