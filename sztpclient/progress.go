/*
SPDX-License-Identifier: Apache-2.0
Copyright (C) 2022-2023 Intel Corporation
Copyright (c) 2022 Dell Inc, or its subsidiaries.
Copyright (C) 2022 Red Hat.
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
	"encoding/json"
	"net/http"
)

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

func (a *Client) postProgress(s ProgressType, message string) {
	a.Logger.Debugf("Starting the Report Progress request for type: %s, message: %s", s, message)
	url := a.BootstrapServerURL + a.BootstrapServerRestconfLink.Href + "/operations/ietf-sztp-bootstrap-server:report-progress"
	var p ProgressJSON
	p.IetfSztpBootstrapServerInput.ProgressType = s.String()
	p.IetfSztpBootstrapServerInput.Message = message

	progressJSON, err := json.Marshal(p)
	if err != nil {
		a.Logger.Errorf("Error marshalling the progress JSON: %s", err.Error())
		return
	}

	res, err := a.postRequest(string(progressJSON), url, true, http.StatusNoContent)
	if err != nil {
		a.Logger.Errorf("Error doing the report for %s: %s", message, err.Error())
		return
	}

	a.Logger.Debug("Response from the server: ", res)
	a.Logger.Debugf("Report Progress request completed successfully for %s", message)
}
