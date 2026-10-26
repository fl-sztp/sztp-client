// SPDX-FileCopyrightText: 2022-2023 Intel Corporation
// SPDX-FileCopyrightText: 2022 Dell Inc, or its subsidiaries.
// SPDX-FileCopyrightText: 2022 Red Hat.
// SPDX-FileCopyrightText: 2025 Anssi Söderena <https://github.com/asode>
// SPDX-FileCopyrightText: 2025 Sandip Ghimire <https://github.com/sandip-ghimire>
// SPDX-License-Identifier: Apache-2.0

// Modified for sztp-client.

package sztpclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
)

// ErrReportCredentials indicates a local configuration failure, not a server
// outage. The caller should stop and retain the pending report for the next run.
var ErrReportCredentials = errors.New("invalid completion report credentials")

type ProgressType int64

const (
	ProgressTypeBootstrapInitiated ProgressType = iota
	ProgressTypeParsingInitiated
	ProgressTypeParsingWarning
	ProgressTypeParsingError
	ProgressTypeParsingComplete
	ProgressTypeBootImageInitiated
	ProgressTypeBootImageWarning
	ProgressTypeBootImageError
	ProgressTypeBootImageMismatch
	ProgressTypeBootImageInstalledRebooting
	ProgressTypeBootImageComplete
	ProgressTypePreScriptInitiated
	ProgressTypePreScriptWarning
	ProgressTypePreScriptError
	ProgressTypePreScriptComplete
	ProgressTypeConfigInitiated
	ProgressTypeConfigWarning
	ProgressTypeConfigError
	ProgressTypeConfigComplete
	ProgressTypePostScriptInitiated
	ProgressTypePostScriptWarning
	ProgressTypePostScriptError
	ProgressTypePostScriptComplete
	ProgressTypeBootstrapWarning
	ProgressTypeBootstrapError
	ProgressTypeBootstrapComplete
	ProgressTypeInformational
)

//nolint:funlen
func (s ProgressType) String() string {
	switch s {
	case ProgressTypeBootstrapInitiated:
		return "bootstrap-initiated"
	case ProgressTypeParsingInitiated:
		return "parsing-initiated"
	case ProgressTypeParsingWarning:
		return "parsing-warning"
	case ProgressTypeParsingError:
		return "parsing-error"
	case ProgressTypeParsingComplete:
		return "parsing-complete"
	case ProgressTypeBootImageInitiated:
		return "boot-image-initiated"
	case ProgressTypeBootImageWarning:
		return "boot-image-warning"
	case ProgressTypeBootImageError:
		return "boot-image-error"
	case ProgressTypeBootImageMismatch:
		return "boot-image-mismatch"
	case ProgressTypeBootImageInstalledRebooting:
		return "boot-image-installed-rebooting"
	case ProgressTypeBootImageComplete:
		return "boot-image-complete"
	case ProgressTypePreScriptInitiated:
		return "pre-script-initiated"
	case ProgressTypePreScriptWarning:
		return "pre-script-warning"
	case ProgressTypePreScriptError:
		return "pre-script-error"
	case ProgressTypePreScriptComplete:
		return "pre-script-complete"
	case ProgressTypeConfigInitiated:
		return "config-initiated"
	case ProgressTypeConfigWarning:
		return "config-warning"
	case ProgressTypeConfigError:
		return "config-error"
	case ProgressTypeConfigComplete:
		return "config-complete"
	case ProgressTypePostScriptInitiated:
		return "post-script-initiated"
	case ProgressTypePostScriptWarning:
		return "post-script-warning"
	case ProgressTypePostScriptError:
		return "post-script-error"
	case ProgressTypePostScriptComplete:
		return "post-script-complete"
	case ProgressTypeBootstrapWarning:
		return "bootstrap-warning"
	case ProgressTypeBootstrapError:
		return "bootstrap-error"
	case ProgressTypeBootstrapComplete:
		return "bootstrap-complete"
	case ProgressTypeInformational:
		return "informational"
	}
	return "unknown"
}

type ProgressJSON struct {
	IetfSztpBootstrapServerInput struct {
		ProgressType string `json:"progress-type"`
		Message      string `json:"message"`
		SSHHostKeys  struct {
			SSHHostKey []struct {
				Algorithm string `json:"algorithm"`
				KeyData   string `json:"key-data"`
			} `json:"ssh-host-key,omitempty"`
		} `json:"ssh-host-keys,omitempty"`
		TrustAnchorCerts struct {
			TrustAnchorCert []string `json:"trust-anchor-cert,omitempty"`
		} `json:"trust-anchor-certs,omitempty"`
	} `json:"ietf-sztp-bootstrap-server:input"`
}

// CompletionReport contains only the identity and final endpoint needed to
// resume reporting after local provisioning has completed. It contains no keys
// or passwords; callers supply current credentials when sending it.
type CompletionReport struct {
	Endpoint     string `json:"endpoint"`
	SerialNumber string `json:"serial_number"`
}

func (r CompletionReport) Validate() error {
	u, err := url.Parse(r.Endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return errors.New("completion report requires an absolute HTTPS endpoint without userinfo or fragment")
	}
	return nil
}

func (a *Client) progressEndpoint() string {
	return a.BootstrapServerURL + a.BootstrapServerRestconfLink.Href + "/operations/ietf-sztp-bootstrap-server:report-progress"
}

// ReportCompletion makes one bounded attempt. Local completion must be saved
// by the caller before this method is invoked.
func (a *Client) ReportCompletion(ctx context.Context, report CompletionReport) error {
	if err := report.Validate(); err != nil {
		return err
	}
	if a.DevicePassword == "" {
		return fmt.Errorf("%w: device password is required", ErrReportCredentials)
	}
	if _, err := tls.LoadX509KeyPair(a.DeviceEndEntityCert, a.DevicePrivateKey); err != nil {
		return fmt.Errorf("%w: %w", ErrReportCredentials, err)
	}
	ca, err := os.ReadFile(a.BootstrapTrustAnchorCert)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrReportCredentials, err)
	}
	if !x509.NewCertPool().AppendCertsFromPEM(ca) {
		return fmt.Errorf("%w: trust anchor contains no certificates", ErrReportCredentials)
	}
	client := *a
	client.SerialNumber = report.SerialNumber
	return client.sendProgress(ctx, report.Endpoint, ProgressTypeBootstrapComplete, "Bootstrap Complete")
}

func (a *Client) postProgress(ctx context.Context, s ProgressType, message string) {
	if err := a.sendProgress(ctx, a.progressEndpoint(), s, message); err != nil {
		a.Logger.Warn("Progress report failed", "progress_type", s.String(), "error", err)
	}
}

func (a *Client) sendProgress(ctx context.Context, endpoint string, s ProgressType, message string) error {
	a.Logger.Debug("Starting progress report", "progress_type", s.String(), "report_message", message)
	var p ProgressJSON
	p.IetfSztpBootstrapServerInput.ProgressType = s.String()
	p.IetfSztpBootstrapServerInput.Message = message

	progressJSON, err := json.Marshal(p)
	if err != nil {
		return err
	}

	res, err := a.postRequestWithContext(ctx, string(progressJSON), endpoint, true, http.StatusNoContent)
	if err != nil {
		return err
	}

	a.Logger.Debug("Progress response received", "response", res)
	a.Logger.Debug("Progress report completed", "progress_type", s.String())
	return nil
}
