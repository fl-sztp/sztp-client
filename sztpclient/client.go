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
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"fl-sztp-client/serveraddr"

	"github.com/github/smimesign/ietf-cms/protocol"
)

func NewClient(logger *slog.Logger, allowInsecure bool, BootstrapServerURL, tmpFolder, serialNumber, devicePassword, devicePrivateKey, deviceEndEntityCert, bootstrapTrustAnchorCert string) *Client {
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

func (a *Client) RunBootstrapSequence(ctx context.Context) (CompletionReport, error) {
	if err := ctx.Err(); err != nil {
		return CompletionReport{}, err
	}
	var bootstrapError error
	var errMessage string

	deviceErr := a.getDeviceInformation()
	if deviceErr != nil {
		a.Logger.Warn("Error getting device information", "error", deviceErr)
	}
	// TODO: fail if we don't have serial number?

	bootstrapError = a.getRestconfLink(ctx)
	if bootstrapError != nil {
		return CompletionReport{}, fmt.Errorf("failed to get host metadata: %w", bootstrapError)
	}

	onboarding, bootstrapError := a.getBootstrappingData(ctx)
	if bootstrapError != nil {
		errMessage = "failed to request bootstrap server onboarding info: " + bootstrapError.Error()
		a.postProgress(ctx, ProgressTypeBootstrapError, errMessage)
		return CompletionReport{}, fmt.Errorf("failed to request bootstrap server onboarding info: %w", bootstrapError)
	}

	if !onboarding {
		bootstrapError = a.getBootstrapRedirect(ctx)
		if bootstrapError != nil {
			errMessage = "failed to handle bootstrap redirect: " + bootstrapError.Error()
			a.postProgress(ctx, ProgressTypeBootstrapError, errMessage)
			return CompletionReport{}, fmt.Errorf("failed to handle bootstrap redirect: %w", bootstrapError)
		}
	}

	// TODO: Download the boot image

	onboardingDir, bootstrapError := a.prepareOnboardingArtifacts(ctx)
	if bootstrapError != nil {
		errMessage = "failed to prepare onboarding files: " + bootstrapError.Error()
		a.postProgress(ctx, ProgressTypeBootstrapError, errMessage)
		return CompletionReport{}, errors.New(errMessage)
	}

	bootstrapError = a.runConfiguration(ctx, PRE, onboardingDir)
	if bootstrapError != nil {
		a.postProgress(ctx, ProgressTypeBootstrapError, "Error launching pre scripts: "+bootstrapError.Error())
		return CompletionReport{}, fmt.Errorf("run pre configuration script: %w", bootstrapError)
	}

	bootstrapError = a.runConfiguration(ctx, POST, onboardingDir)
	if bootstrapError != nil {
		a.postProgress(ctx, ProgressTypeBootstrapError, "Error launching post scripts: "+bootstrapError.Error())
		return CompletionReport{}, fmt.Errorf("run post configuration script: %w", bootstrapError)
	}

	report := CompletionReport{Endpoint: a.progressEndpoint(), SerialNumber: a.SerialNumber}
	return report, report.Validate()
}

func (a *Client) getRestconfLink(ctx context.Context) error {
	url := a.BootstrapServerURL + "/.well-known/host-meta"
	a.Logger.Debug("Getting root resources", "url", url)

	xrd, err := a.getHostMeta(ctx)
	if err != nil {
		return err
	}
	a.Logger.Debug("Host metadata received", "response", xrd)

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

func (a *Client) getBootstrapRedirect(ctx context.Context) error {
	servers := a.BootstrapServerRedirectInfo.IetfSztpConveyedInfoRedirectInformation.BootstrapServer
	if len(servers) == 0 {
		return errors.New("redirect information contains no bootstrap server")
	}

	a.Logger.Info("Redirecting to bootstrap server")

	// TODO: BootstrapServer can be an array
	// TODO: do not ignore BootstrapServer[0].TrustAnchor
	redirectURL, err := normalizeRedirectAddress(servers[0].Address, servers[0].Port)
	if err != nil {
		return fmt.Errorf("invalid bootstrap redirect: %w", err)
	}
	a.BootstrapServerURL = redirectURL

	// Request onboard info again (with new URL now)
	onboarding, err := a.getBootstrappingData(ctx)
	if err != nil {
		return err
	}
	if !onboarding {
		return errors.New("redirect response did not contain onboarding information")
	}
	return nil
}

// normalizeRedirectAddress accepts a host or URL and a separate authoritative
// port. Both the original address and the resulting origin use shared validation.
func normalizeRedirectAddress(address string, port int) (string, error) {
	address = strings.TrimSpace(address)
	if ip, err := netip.ParseAddr(address); err == nil && ip.Is6() {
		address = "[" + address + "]"
	}
	origin, err := serveraddr.Normalize(address)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(origin)
	if err != nil {
		return "", err
	}
	u.Host = net.JoinHostPort(u.Hostname(), strconv.Itoa(port))
	return serveraddr.Normalize(u.String())
}

func (a *Client) getBootstrappingData(ctx context.Context) (bool, error) {

	url := a.BootstrapServerURL + a.BootstrapServerRestconfLink.Href + "/operations/ietf-sztp-bootstrap-server:get-bootstrapping-data"

	res, err := a.postRequestWithContext(ctx, a.InputJSONContent, url, false, http.StatusOK)
	if err != nil {
		return false, err
	}

	a.Logger.Debug("Bootstrap response received", "response", res)

	a.postProgress(ctx, ProgressTypeBootstrapInitiated, "Bootstrap process initiated")

	crypto := res.IetfSztpBootstrapServerOutput.ConveyedInformation
	newVal, err := base64.StdEncoding.DecodeString(crypto)
	if err != nil {
		return false, err
	}
	ci, err := protocol.ParseContentInfo(newVal)
	if err != nil {
		return false, err
	}
	var data asn1.RawValue
	_, kerr := asn1.Unmarshal(ci.Content.Bytes, &data)
	if kerr != nil {
		return false, kerr
	}

	return a.decodeConveyedInformation(data.Bytes)
}

// Envelope presence is independent of its contents: an explicit empty onboarding
// object is valid, but a missing or null envelope must never mark a device done.
func (a *Client) decodeConveyedInformation(data []byte) (bool, error) {
	a.BootstrapServerOnboardingInfo = BootstrapServerOnboardingInfo{}
	a.BootstrapServerRedirectInfo = BootstrapServerRedirectInfo{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil {
		return false, err
	}
	if start != json.Delim('{') || !decoder.More() {
		return false, errors.New("expected exactly one onboarding-information or redirect-information envelope")
	}
	name, err := decoder.Token()
	if err != nil {
		return false, err
	}
	var contents json.RawMessage
	if err := decoder.Decode(&contents); err != nil {
		return false, err
	}
	// Count members before decoding into a map or struct can merge duplicate keys.
	if decoder.More() {
		return false, errors.New("expected exactly one onboarding-information or redirect-information envelope")
	}
	if _, err := decoder.Token(); err != nil { // Closing object delimiter.
		return false, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return false, errors.New("unexpected data after conveyed-information envelope")
	}
	if bytes.Equal(bytes.TrimSpace(contents), []byte("null")) {
		return false, errors.New("conveyed-information envelope must not be null")
	}
	decoder = json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	switch name {
	case "ietf-sztp-conveyed-info:onboarding-information":
		var oi BootstrapServerOnboardingInfo
		if err := decoder.Decode(&oi.IetfSztpConveyedInfoOnboardingInformation); err != nil {
			return false, err
		}
		a.BootstrapServerOnboardingInfo = oi
		a.Logger.Debug("Onboarding information received", "response", a.BootstrapServerOnboardingInfo)
		return true, nil
	case "ietf-sztp-conveyed-info:redirect-information":
		var ri BootstrapServerRedirectInfo
		if err := decoder.Decode(&ri.IetfSztpConveyedInfoRedirectInformation); err != nil {
			return false, err
		}
		a.BootstrapServerRedirectInfo = ri
		a.Logger.Debug("Redirect information received", "response", a.BootstrapServerRedirectInfo)
		return false, nil
	}
	return false, errors.New("expected onboarding-information or redirect-information envelope")
}
