/*
 * Copyright 2016-2023 Hedera Hashgraph, LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package detect

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// darwinOSDetector implements OSDetector interface for darwin like OS
type darwinOSDetector struct {
}

// detectDarwinFlavor converts release ID into a Mac flavor.
func (dd *darwinOSDetector) detectDarwinFlavor(productVersion string) string {
	productVersion = strings.ToLower(productVersion)
	parts := strings.Split(productVersion, ".")

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return OSFlavorUnknown
	}

	if major > 10 {
		productVersion = fmt.Sprintf("%d.*", major)
	}

	if flavor, found := macFlavorMapping[productVersion]; found {
		return flavor
	}

	return OSFlavorUnknown
}

// ScanOS returns OSInfo include macOS version, release and codeName.
// It requires `uname` and `sw_vers` program to be available.
func (dd *darwinOSDetector) ScanOS() (*OSInfo, error) {
	osInfo := OSInfo{
		Type:         runtime.GOOS,
		Architecture: runtime.GOARCH,
		Version:      "",
		Flavor:       "",
		CodeName:     "",
	}

	// detect version
	command := exec.Command("uname", "-r")
	output, err := command.Output()
	if err == nil {
		osInfo.Version = strings.Trim(string(output), "\n")
	}

	// detect flavor
	command = exec.Command("sw_vers", "-productVersion")
	output, err = command.Output()
	if err == nil {
		productVersion := strings.Trim(string(output), "\n")
		osInfo.Flavor = dd.detectDarwinFlavor(productVersion)
	}

	// codename and flavor are same for macOS
	osInfo.CodeName = osInfo.Flavor

	return &osInfo, nil
}

func NewDarwinOSDetector() OSDetector {
	return &darwinOSDetector{}
}
