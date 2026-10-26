// SPDX-FileCopyrightText: 2026 Anssi Söderena <https://github.com/asode>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"time"
)

func reportCertificates(r *reportWriter, config *Config) {
	r.value("Client certificate file", config.DeviceEndEntityCert, "")
	reportCertificateFile(r, "Client certificate", config.DeviceEndEntityCert)
	r.value("Client private key file", config.DevicePrivateKey, "")
	if config.DeviceEndEntityCert == "" || config.DevicePrivateKey == "" {
		r.printf("Client certificate/key pair: not configured completely\n")
	} else if _, err := tls.LoadX509KeyPair(config.DeviceEndEntityCert, config.DevicePrivateKey); err != nil {
		r.printf("Client certificate/key pair: cannot be loaded: %q\n", err.Error())
	} else {
		r.printf("Client certificate/key pair: loadable, key matches certificate\n")
	}
	r.value("Trust anchor file", config.BootstrapTrustAnchor, "")
	reportCertificateFile(r, "Trust anchor", config.BootstrapTrustAnchor)
	if config.AllowInsecure {
		r.printf("Server certificate verification: disabled (insecure=true); trust anchors are not enforced\n")
	} else {
		r.printf("Server certificate verification: enabled (insecure=false)\n")
	}
}

func reportCertificateFile(r *reportWriter, label, path string) {
	if path == "" {
		return
	}
	certificates, err := readCertificates(path)
	now := time.Now()
	for i, cert := range certificates {
		validity := "within validity period"
		if now.Before(cert.NotBefore) {
			validity = "not yet valid"
		} else if now.After(cert.NotAfter) {
			validity = "expired"
		}
		r.printf("%s %d:\n", label, i+1)
		r.printf("  Subject: %q\n  Issuer: %q\n", cert.Subject.String(), cert.Issuer.String())
		r.printf("  Validity: %s to %s (%s)\n", cert.NotBefore.UTC().Format(time.RFC3339), cert.NotAfter.UTC().Format(time.RFC3339), validity)
		r.printf("  SHA-256: %x\n", sha256.Sum256(cert.Raw))
	}
	if err != nil {
		r.printf("%s inspection error: %q\n", label, err.Error())
	}
}

// readCertificates returns readable certificates even if another certificate
// in the PEM bundle is malformed. Other PEM block types are never printed.
func readCertificates(path string) ([]*x509.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var certificates []*x509.Certificate
	var parseErr error
	for {
		block, rest := pem.Decode(data)
		if block == nil {
			break
		}
		data = rest
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			parseErr = fmt.Errorf("invalid X.509 certificate: %w", err)
			continue
		}
		certificates = append(certificates, cert)
	}
	if len(certificates) == 0 && parseErr == nil {
		parseErr = fmt.Errorf("no readable PEM certificates found")
	}
	return certificates, parseErr
}
