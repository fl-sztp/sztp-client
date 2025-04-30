/*
SPDX-License-Identifier: Apache-2.0
Copyright (C) 2022-2023 Intel Corporation
Copyright (c) 2022 Dell Inc, or its subsidiaries
Copyright (C) 2022 Red Hat
Copyright (C) 2025 Anssi Söderena, Sandip Ghimire

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

package sztpclient

import (
	"encoding/xml"

	"github.com/sirupsen/logrus"
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
	Logger                        *logrus.Logger
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
