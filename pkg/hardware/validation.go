// SPDX-License-Identifier: Apache-2.0

package hardware

import (
	"strconv"
	"strings"
)

// validateOS validates the system OS against the supported OS requirements
func validateOS(minSupportedOS []string, systemInfo HostProfile) bool {
	for _, supportedOS := range minSupportedOS {
		if isOSSupported(supportedOS, systemInfo) {
			return true
		}
	}
	return false
}

// isOSSupported checks if the system OS matches a supported OS specification
// Supports formats like "Ubuntu 18", "Debian 10"
func isOSSupported(supportedOS string, systemInfo HostProfile) bool {
	parts := strings.Fields(strings.ToLower(supportedOS))
	if len(parts) < 2 {
		return false
	}

	currentOs := parts[0]
	currentVersion := parts[1]

	// Check if OS name matches
	if !matchesOSName(currentOs, systemInfo.GetOSVendor()) {
		return false
	}

	minVersion, err := strconv.Atoi(currentVersion)
	if err != nil {
		return false
	}

	// Parse system version
	systemVersionParts := strings.Split(systemInfo.GetOSVersion(), ".")
	if len(systemVersionParts) == 0 {
		return false
	}

	systemMajorVersion, err := strconv.Atoi(systemVersionParts[0])
	if err != nil {
		return false
	}

	return systemMajorVersion >= minVersion
}

// matchesOSName checks if the OS name matches the system vendor
func matchesOSName(osName string, systemVendor string) bool {
	return strings.EqualFold(osName, systemVendor)
}
