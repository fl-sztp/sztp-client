// SPDX-FileCopyrightText: 2022-2023 Intel Corporation
// SPDX-FileCopyrightText: 2022 Dell Inc, or its subsidiaries
// SPDX-FileCopyrightText: 2022 Red Hat
// SPDX-FileCopyrightText: 2025 Anssi Söderena <https://github.com/asode>
// SPDX-FileCopyrightText: 2025 Sandip Ghimire <https://github.com/sandip-ghimire>
// SPDX-License-Identifier: Apache-2.0

// Modified for sztp-client.

package sztpclient

import (
	"encoding/xml"
	"log/slog"
)

type XRD struct {
	XMLName xml.Name `xml:"XRD"`
	Links   []Link   `xml:"Link"`
}

type Link struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
}

type InputJSON struct {
	IetfSztpBootstrapServerInput struct {
		HwModel   string `json:"hw-model"`
		OsName    string `json:"os-name"`
		OsVersion string `json:"os-version"`
		Nonce     string `json:"nonce"`
	} `json:"ietf-sztp-bootstrap-server:input"`
}

type BootstrapServerRedirectInfo struct {
	IetfSztpConveyedInfoRedirectInformation struct {
		BootstrapServer []struct {
			Address     string `json:"address"`
			Port        int    `json:"port"`
			TrustAnchor string `json:"trust-anchor"`
		} `json:"bootstrap-server"`
	} `json:"ietf-sztp-conveyed-info:redirect-information"`
}

type BootstrapServerOnboardingInfo struct {
	IetfSztpConveyedInfoOnboardingInformation struct {
		InfoTimestampReference string // [not received in json] This is the reference to know exactly the time file downloaded and reference to the artifacts of a specific request
		BootImage              struct {
			DownloadURI       []string `json:"download-uri"`
			ImageVerification []struct {
				HashAlgorithm string `json:"hash-algorithm"`
				HashValue     string `json:"hash-value"`
			} `json:"image-verification"`
		} `json:"boot-image"`
		PreConfigurationScript  string `json:"pre-configuration-script"`
		ConfigurationHandling   string `json:"configuration-handling"`
		Configuration           string `json:"configuration"`
		PostConfigurationScript string `json:"post-configuration-script"`
	} `json:"ietf-sztp-conveyed-info:onboarding-information"`
}

type BootstrapServerPostOutput struct {
	IetfSztpBootstrapServerOutput struct {
		ConveyedInformation string `json:"conveyed-information"`
	} `json:"ietf-sztp-bootstrap-server:output"`
}

type BootstrapServerErrorOutput struct {
	IetfRestconfErrors struct {
		Error []struct {
			ErrorType    string `json:"error-type"`
			ErrorTag     string `json:"error-tag"`
			ErrorMessage string `json:"error-message"`
		} `json:"error"`
	} `json:"ietf-restconf:errors"`
}

type Client struct {
	Logger                        *slog.Logger
	AllowInsecure                 bool                          // Allow insecure connections (skip SSL certificate verification)
	BootstrapServerURL            string                        // Bootstrap complete URL
	TmpFolder                     string                        // Temporary folder
	SerialNumber                  string                        // Device's Serial Number
	DevicePassword                string                        // Device's Password
	DevicePrivateKey              string                        // Device's private key
	DeviceEndEntityCert           string                        // Device's end-entity cert
	BootstrapTrustAnchorCert      string                        // the trusted bootstrap server's trust-anchor certificate (PEM)
	ContentTypeReq                string                        // The content type for the request to the Server
	InputJSONContent              string                        // The input.json file serialized
	BootstrapServerRestconfLink   Link                          // The RESTCONF root resource
	BootstrapServerOnboardingInfo BootstrapServerOnboardingInfo // BootstrapServerOnboardingInfo structure
	BootstrapServerRedirectInfo   BootstrapServerRedirectInfo   // BootstrapServerRedirectInfo structure
}
