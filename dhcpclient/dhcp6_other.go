//go:build !linux
// +build !linux

/*
SPDX-License-Identifier: Apache-2.0
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

package dhcpclient

import (
	"fmt"

	"github.com/sirupsen/logrus"
)

func GetServerFromDHCP6(logger *logrus.Logger) (string, error) {
	// TODO Support other OS
	return "", fmt.Errorf("OS not supported")

}
