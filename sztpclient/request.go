// SPDX-FileCopyrightText: 2022-2023 Intel Corporation
// SPDX-FileCopyrightText: 2022 Dell Inc, or its subsidiaries
// SPDX-FileCopyrightText: 2022 Red Hat
// SPDX-FileCopyrightText: 2025 Anssi Söderena <https://github.com/asode>
// SPDX-FileCopyrightText: 2025 Sandip Ghimire <https://github.com/sandip-ghimire>
// SPDX-License-Identifier: Apache-2.0

// Modified for sztp-client.

package sztpclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// HTTPRequestLimits bounds each HTTP exchange, including response reading.
// Zero fields select defaults; negative values are invalid. Set these before
// using a Client. Body limits apply after automatic HTTP decompression.
type HTTPRequestLimits struct {
	HostMetaTimeout           time.Duration
	BootstrapTimeout          time.Duration
	ProgressTimeout           time.Duration
	HostMetaMaxResponseBytes  int64
	BootstrapMaxResponseBytes int64
	ProgressMaxResponseBytes  int64
}

// DefaultHTTPRequestLimits returns finite defaults for all request types.
func DefaultHTTPRequestLimits() HTTPRequestLimits {
	return HTTPRequestLimits{
		HostMetaTimeout: 30 * time.Second, BootstrapTimeout: 120 * time.Second, ProgressTimeout: 30 * time.Second,
		HostMetaMaxResponseBytes: 1 << 20, BootstrapMaxResponseBytes: 16 << 20, ProgressMaxResponseBytes: 64 << 10,
	}
}

// Validate rejects invalid limits without changing zero fields to defaults.
func (l HTTPRequestLimits) Validate() error {
	for _, limit := range []struct {
		name    string
		timeout time.Duration
		bytes   int64
	}{
		{"host-meta", l.HostMetaTimeout, l.HostMetaMaxResponseBytes},
		{"bootstrap", l.BootstrapTimeout, l.BootstrapMaxResponseBytes},
		{"progress", l.ProgressTimeout, l.ProgressMaxResponseBytes},
	} {
		if limit.timeout < 0 {
			return fmt.Errorf("%s-timeout must be positive or zero for the default", limit.name)
		}
		// Reserve one byte for overflow detection and keep lengths representable
		// as int on the target architecture, as required by Go slices.
		if limit.bytes < 0 || uint64(limit.bytes) >= uint64(^uint(0)>>1) {
			return fmt.Errorf("%s-max-response-bytes must be nonnegative and less than %d", limit.name, uint64(^uint(0)>>1))
		}
	}
	return nil
}

func (l HTTPRequestLimits) withDefaults() HTTPRequestLimits {
	d := DefaultHTTPRequestLimits()
	if l.HostMetaTimeout != 0 {
		d.HostMetaTimeout = l.HostMetaTimeout
	}
	if l.BootstrapTimeout != 0 {
		d.BootstrapTimeout = l.BootstrapTimeout
	}
	if l.ProgressTimeout != 0 {
		d.ProgressTimeout = l.ProgressTimeout
	}
	if l.HostMetaMaxResponseBytes != 0 {
		d.HostMetaMaxResponseBytes = l.HostMetaMaxResponseBytes
	}
	if l.BootstrapMaxResponseBytes != 0 {
		d.BootstrapMaxResponseBytes = l.BootstrapMaxResponseBytes
	}
	if l.ProgressMaxResponseBytes != 0 {
		d.ProgressMaxResponseBytes = l.ProgressMaxResponseBytes
	}
	return d
}

// ErrResponseTooLarge indicates that a response exceeded its configured limit.
var ErrResponseTooLarge = errors.New("HTTP response exceeds size limit")

func readResponseBody(body io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w (%d bytes)", ErrResponseTooLarge, maxBytes)
	}
	if err != nil {
		return nil, err
	}
	return data, nil
}

// requireHTTPS enforces the RESTCONF transport requirement (RFC 8040, 2.1)
// and authenticated host metadata retrieval (RFC 6415, 5).
func requireHTTPS(req *http.Request) error {
	if req.URL.Scheme != "https" {
		return errors.New("HTTPS is required for SZTP requests")
	}
	return nil
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	// A custom redirect policy replaces Go's default request limit.
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	return requireHTTPS(req)
}

func (a *Client) getHostMeta(ctx context.Context) (XRD, error) {
	if err := a.RequestLimits.Validate(); err != nil {
		return XRD{}, err
	}
	limits := a.RequestLimits.withDefaults()
	ctx, cancel := context.WithTimeout(ctx, limits.HostMetaTimeout)
	defer cancel()
	url := a.BootstrapServerURL + "/.well-known/host-meta"

	r, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return XRD{}, err
	}
	if err := requireHTTPS(r); err != nil {
		return XRD{}, err
	}

	r.Header.Add("Accept", "application/xrd+xml")
	// r.SetBasicAuth(a.SerialNumber, a.DevicePassword)

	caCert, _ := os.ReadFile(a.BootstrapTrustAnchorCert)
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)
	cert, _ := tls.LoadX509KeyPair(a.DeviceEndEntityCert, a.DevicePrivateKey)

	client := &http.Client{
		CheckRedirect: checkRedirect,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:            caCertPool,
				Certificates:       []tls.Certificate{cert},
				InsecureSkipVerify: a.AllowInsecure,
			},
		},
	}

	defer client.CloseIdleConnections()
	res, err := client.Do(r)
	if err != nil {
		return XRD{}, err
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return XRD{}, fmt.Errorf("unexpected response status code: %d, expected 200 OK", res.StatusCode)
	}

	body, err := readResponseBody(res.Body, limits.HostMetaMaxResponseBytes)
	if err != nil {
		return XRD{}, fmt.Errorf("error reading response body: %w", err)
	}

	var xrd XRD
	err = xml.Unmarshal(body, &xrd)
	if err != nil {
		return XRD{}, fmt.Errorf("error unmarshalling XML: %w", err)
	}

	return xrd, nil
}

func (a *Client) postRequestWithContext(ctx context.Context, input string, url string, empty bool, expectedStatusCode int) (*BootstrapServerPostOutput, error) {
	if err := a.RequestLimits.Validate(); err != nil {
		return nil, err
	}
	limits := a.RequestLimits.withDefaults()
	timeout, maxBytes := limits.BootstrapTimeout, limits.BootstrapMaxResponseBytes
	if empty {
		timeout, maxBytes = limits.ProgressTimeout, limits.ProgressMaxResponseBytes
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var postResponse BootstrapServerPostOutput
	var errorResponse BootstrapServerErrorOutput

	a.Logger.Debug("Starting request", "url", url, "input", input)

	body := strings.NewReader(input)
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}
	if err := requireHTTPS(r); err != nil {
		return nil, err
	}

	r.SetBasicAuth(a.SerialNumber, a.DevicePassword)
	r.Header.Add("Content-Type", a.ContentTypeReq)
	r.Header.Add("Accept", a.ContentTypeReq)

	caCert, _ := os.ReadFile(a.BootstrapTrustAnchorCert)
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)
	cert, _ := tls.LoadX509KeyPair(a.DeviceEndEntityCert, a.DevicePrivateKey)

	client := &http.Client{
		CheckRedirect: checkRedirect,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:            caCertPool,
				Certificates:       []tls.Certificate{cert},
				InsecureSkipVerify: a.AllowInsecure,
			},
		},
	}
	defer client.CloseIdleConnections()
	res, err := client.Do(r)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := res.Body.Close(); err != nil {
			a.Logger.Warn("Failed to close response body", "url", url, "error", err)
		}
	}()

	bodyBytes, err := readResponseBody(res.Body, maxBytes)
	if err != nil {
		return nil, err
	}

	decoder := json.NewDecoder(bytes.NewReader(bodyBytes))
	decoder.DisallowUnknownFields()
	if !empty {
		derr := decoder.Decode(&postResponse)
		if derr != nil {
			errdecoder := json.NewDecoder(bytes.NewReader(bodyBytes))
			errdecoder.DisallowUnknownFields()
			eerr := errdecoder.Decode(&errorResponse)
			if eerr != nil {
				return nil, fmt.Errorf("%w; response body: %s", derr, bodyBytes)
			}
			if len(errorResponse.IetfRestconfErrors.Error) > 0 {
				errDetails := errorResponse.IetfRestconfErrors.Error[0]
				return nil, fmt.Errorf("expected conveyed-information, received error type=%s, tag=%s, message=%s",
					errDetails.ErrorType, errDetails.ErrorTag, errDetails.ErrorMessage)
			}
			return nil, errors.New("expected conveyed-information, but no error details were provided")
		}
	}

	if res.StatusCode != expectedStatusCode {
		return nil, errors.New("Status code received: " + strconv.Itoa(res.StatusCode) + " ...but status code expected: " + strconv.Itoa(expectedStatusCode))
	}
	return &postResponse, nil
}
