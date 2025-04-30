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
	"bytes"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/github/smimesign/ietf-cms/protocol"
	"github.com/sirupsen/logrus"
)

func NewClient(logger *logrus.Logger, allowInsecure bool, BootstrapServerURL, tmpFolder, serialNumber, devicePassword, devicePrivateKey, deviceEndEntityCert, bootstrapTrustAnchorCert string) *Client {
	return &Client{
		Logger:                        logger,
		AllowInsecure:                 allowInsecure,
		BootstrapServerURL:            BootstrapServerURL,
		TmpFolder:                     tmpFolder,
		SerialNumber:                  serialNumber,
		DevicePassword:                devicePassword,
		DevicePrivateKey:              devicePrivateKey,
		DeviceEndEntityCert:           deviceEndEntityCert,
		BootstrapTrustAnchorCert:      bootstrapTrustAnchorCert,
		ContentTypeReq:                "application/yang-data+json",
		InputJSONContent:              "",
		BootstrapServerRestconfLink:   Link{},
		BootstrapServerRedirectInfo:   BootstrapServerRedirectInfo{},
		BootstrapServerOnboardingInfo: BootstrapServerOnboardingInfo{},
	}
}

const (
	PRE  = "pre"
	POST = "post"
)

func (a *Client) RunBootstrapSequence() error {
	var bootstrapError error
	var errMessage string

	deviceErr := a.getDeviceInformation()
	if deviceErr != nil {
		a.Logger.Warn("Error getting device information: ", deviceErr)
	}
	// TODO: fail if we don't have serial number?

	bootstrapError = a.getRestconfLink()
	if bootstrapError != nil {
		errMessage = "failed to get host metadata: " + bootstrapError.Error()
		return errors.New(errMessage)
	}

	bootstrapError = a.getBootstrappingData()
	if bootstrapError != nil {
		errMessage = "failed to request bootstrap server onboarding info: " + bootstrapError.Error()
		a.postProgress(ProgressTypeBootstrapError, errMessage)
		return errors.New(errMessage)
	}

	bootstrapError = a.getBootstrapRedirect()
	if bootstrapError != nil {
		errMessage = "failed to handle bootstrap redirect: " + bootstrapError.Error()
		a.postProgress(ProgressTypeBootstrapError, errMessage)
		return errors.New(errMessage)
	}

	// TODO: Download the boot image

	bootstrapError = a.getConfigurationFile()
	if bootstrapError != nil {
		errMessage = "failed to copy configuration file: " + bootstrapError.Error()
		a.postProgress(ProgressTypeBootstrapError, errMessage)
		return errors.New(errMessage)
	}

	bootstrapError = a.runConfiguration(PRE)
	if bootstrapError != nil {
		a.Logger.Error("Error launching pre scripts: ", bootstrapError)
		a.postProgress(ProgressTypeBootstrapError, "Error launching pre scripts: "+bootstrapError.Error())
		return bootstrapError
	}

	bootstrapError = a.runConfiguration(POST)
	if bootstrapError != nil {
		a.Logger.Error("Error launching post scripts: ", bootstrapError)
		a.postProgress(ProgressTypeBootstrapError, "Error launching post scripts: "+bootstrapError.Error())
		return bootstrapError
	}

	a.postProgress(ProgressTypeBootstrapComplete, "Bootstrap Complete")
	return nil
}

func (a *Client) getRestconfLink() error {
	url := a.BootstrapServerURL + "/.well-known/host-meta"
	a.Logger.Debug("Getting root resources from URL:", url)

	xrd, err := a.getHostMeta()
	if err != nil {
		return err
	}
	a.Logger.Debug("Response from the server: ", xrd)

	if len(xrd.Links) == 0 {
		return errors.New("no links found in the root resource response")
	}

	// find the restconf path
	for _, link := range xrd.Links {
		// TODO: We could use other restconf versions
		if link.Rel == "restconf" {
			a.BootstrapServerRestconfLink = link
			break
		}
	}

	return nil
}

func (a *Client) getBootstrapRedirect() error {
	if reflect.ValueOf(a.BootstrapServerRedirectInfo).IsZero() {
		return nil
	}

	a.Logger.Info("Redirecting to bootstrap server")

	// TODO: BootstrapServer can be an array
	// TODO: do not ignore BootstrapServer[0].TrustAnchor
	addr := a.BootstrapServerRedirectInfo.IetfSztpConveyedInfoRedirectInformation.BootstrapServer[0].Address
	port := a.BootstrapServerRedirectInfo.IetfSztpConveyedInfoRedirectInformation.BootstrapServer[0].Port

	if addr == "" {
		return errors.New("invalid redirect address")
	}
	if port <= 0 {
		return errors.New("invalid port")
	}

	// Use (force) HTTPS for the request
	if strings.HasPrefix(addr, "http://") {
		addr = "https://" + strings.TrimPrefix(addr, "http://")
	} else if !strings.HasPrefix(addr, "https://") {
		addr = "https://" + addr
	}
	// Change URL to point to new redirect IP and PORT
	a.BootstrapServerURL = addr + ":" + strconv.Itoa(port)

	// Request onboard info again (with new URL now)
	return a.getBootstrappingData()
}

func (a *Client) getBootstrappingData() error {

	url := a.BootstrapServerURL + a.BootstrapServerRestconfLink.Href + "/operations/ietf-sztp-bootstrap-server:get-bootstrapping-data"

	res, err := a.postRequest(a.InputJSONContent, url, false, http.StatusOK)
	if err != nil {
		return err
	}

	a.Logger.Debug("Response from the server: ", res)

	a.postProgress(ProgressTypeBootstrapInitiated, "Bootstrap process initiated")

	crypto := res.IetfSztpBootstrapServerOutput.ConveyedInformation
	newVal, err := base64.StdEncoding.DecodeString(crypto)
	if err != nil {
		return err
	}
	ci, err := protocol.ParseContentInfo(newVal)
	if err != nil {
		return err
	}
	var data asn1.RawValue
	_, kerr := asn1.Unmarshal(ci.Content.Bytes, &data)
	if kerr != nil {
		return kerr
	}

	res.IetfSztpBootstrapServerOutput.ConveyedInformation = string(data.Bytes)
	decoderoi := json.NewDecoder(bytes.NewReader(data.Bytes))
	decoderoi.DisallowUnknownFields()
	var oi BootstrapServerOnboardingInfo
	erroi := decoderoi.Decode(&oi)
	if erroi == nil {
		a.BootstrapServerOnboardingInfo = oi
		a.Logger.Debug("The BootstrapServerOnBoardingInfo object retrieved is: ", a.BootstrapServerOnboardingInfo)
		return nil
	}

	decoderri := json.NewDecoder(bytes.NewReader(data.Bytes))
	decoderri.DisallowUnknownFields()
	var ri BootstrapServerRedirectInfo
	errri := decoderri.Decode(&ri)
	if errri == nil {
		a.BootstrapServerRedirectInfo = ri
		a.Logger.Debug("The BootstrapServerRedirectInfo object retrieved is: ", a.BootstrapServerRedirectInfo)
		return nil
	}

	return errri
}
