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
)

func (a *Client) getHostMeta() (XRD, error) {
	url := a.BootstrapServerURL + "/.well-known/host-meta"

	r, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return XRD{}, err
	}

	r.Header.Add("Accept", "application/xrd+xml")
	// r.SetBasicAuth(a.SerialNumber, a.DevicePassword)

	caCert, _ := os.ReadFile(a.BootstrapTrustAnchorCert)
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)
	cert, _ := tls.LoadX509KeyPair(a.DeviceEndEntityCert, a.DevicePrivateKey)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:            caCertPool,
				Certificates:       []tls.Certificate{cert},
				InsecureSkipVerify: a.AllowInsecure,
			},
		},
	}

	res, err := client.Do(r)
	if err != nil {
		return XRD{}, err
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return XRD{}, fmt.Errorf("unexpected response status code: %d, expected 200 OK", res.StatusCode)
	}

	body, err := io.ReadAll(res.Body)
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

func (a *Client) postRequest(input string, url string, empty bool, expectedStatusCode int) (*BootstrapServerPostOutput, error) {
	var postResponse BootstrapServerPostOutput
	var errorResponse BootstrapServerErrorOutput

	a.Logger.Debug(fmt.Sprintf("Starting the request to %s with input: %s", url, input))

	body := strings.NewReader(input)
	r, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
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
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:            caCertPool,
				Certificates:       []tls.Certificate{cert},
				InsecureSkipVerify: a.AllowInsecure,
			},
		},
	}
	res, err := client.Do(r)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := res.Body.Close(); err != nil {
		}
	}()

	bodyBytes, err := io.ReadAll(res.Body)
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
