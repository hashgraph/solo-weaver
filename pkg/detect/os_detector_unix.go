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
	"bufio"
	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
	"os"
	"regexp"
	"runtime"
	"strings"
)

// unixOSDetector implements OSDetector interface for unix like OS
type unixOSDetector struct {
	// list of files to check in order
	// files are mapped in osReleaseFilePaths
	fileCheckSequence []string

	// mapping of release file name to path
	osReleaseFilePaths map[string]string

	logger zerolog.Logger
}

// extractVal extracts the value part from the line.
// It assumes the value is separated by '=' and returns the second part after trimming spaces
func (ud *unixOSDetector) extractVal(line string) string {
	parts := strings.Split(line, "=")
	if len(parts) == 2 {
		return strings.Trim(strings.TrimSpace(parts[1]), "\"")
	}

	return ""
}

// detectLinuxFlavor converts release ID into a Linux OS flavor.
// release ID should be extracted from /etc/lsb-release or etc/os-release file.
func (ud *unixOSDetector) detectLinuxFlavor(releaseID string) string {
	releaseID = strings.ToLower(releaseID)
	if flavor, found := linuxFlavorMapping[releaseID]; found {
		return flavor
	}

	return OSFlavorUnknown
}

// extractOSInfo is a helper method to extract version, flavor and  codeName from a release file.
//
// It assumes the path points to a /etc/lsb-release or /etc/os-release file.
//
// It performs basic string prefix matching to determine which line has the expected values.
//
// If no values are found, it returns a OSInfo instance with OS Type and OS Architecture info only as returned by Go
// runtime.
func (ud *unixOSDetector) extractOSInfo(path string, idPrefix string, releasePrefix string,
	codeNamePrefix string) (*OSInfo, error) {

	f, err := os.Open(path)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid OS file %q", path)
	}
	defer f.Close()

	osInfo := &OSInfo{
		Type:         runtime.GOOS,
		Architecture: runtime.GOARCH,
	}

	// detect version, flavor and codename
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, releasePrefix) {
			version := ud.extractVal(line)
			osInfo.Version = version
		} else if strings.HasPrefix(line, idPrefix) {
			releaseID := ud.extractVal(line)
			osInfo.Flavor = ud.detectLinuxFlavor(releaseID)
		} else if strings.HasPrefix(line, codeNamePrefix) {
			osInfo.CodeName = ud.extractVal(line)
		}

		if osInfo.Version != "" && osInfo.Flavor != "" && osInfo.CodeName != "" {
			break
		}
	}

	return osInfo, nil

}

func (ud *unixOSDetector) scanRedhatReleaseFile(path string, versionRegex string) (*OSInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid OS release file %q", path)
	}
	defer f.Close()

	matcher, err := regexp.Compile(versionRegex)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse release version regex %q", versionRegex)
	}

	osInfo := &OSInfo{
		Type:         runtime.GOOS,
		Flavor:       OSFlavorLinuxRhel,
		Architecture: runtime.GOARCH,
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Red Hat Enterprise Linux release") {
			matches := matcher.FindStringSubmatch(line)

			if len(matches) == 6 { // 6 for both version and codename
				var versionParts []string
				for _, part := range matches[1 : len(matches)-1] {
					if part != "" {
						versionParts = append(versionParts, part)
					}
				}
				osInfo.Version = strings.Join(versionParts, ".")
				osInfo.CodeName = matches[len(matches)-1]
			}

			break
		}
	}

	return osInfo, nil
}

func (ud *unixOSDetector) scanLSBReleaseFile(path string) (*OSInfo, error) {
	return ud.extractOSInfo(path, "DISTRIB_ID=", "DISTRIB_RELEASE=", "DISTRIB_CODENAME=")
}

func (ud *unixOSDetector) scanOSReleaseFile(path string) (*OSInfo, error) {
	return ud.extractOSInfo(path,
		"ID=", "VERSION_ID=", "VERSION_CODENAME=")
}

func (ud *unixOSDetector) scanReleaseFile(releaseFileName string, path string) (*OSInfo, error) {
	switch releaseFileName {
	case RedhatReleaseFileName:
		return ud.scanRedhatReleaseFile(path, RedhatVersionRegex)
	case LSBReleaseFileName:
		return ud.scanLSBReleaseFile(path)
	case OSReleaseFileName:
		return ud.scanOSReleaseFile(path)
	}

	return nil, errors.Newf("unsupported OS release file %q", path)
}

func (ud *unixOSDetector) ScanOS() (*OSInfo, error) {
	var paths []string

	for _, releaseFileName := range ud.fileCheckSequence {
		if path, found := ud.osReleaseFilePaths[releaseFileName]; found {
			paths = append(paths, path)
			ud.logger.Debug().Msgf("Processing %q at %q", releaseFileName, path)
			osInfo, err := ud.scanReleaseFile(releaseFileName, path)
			if err == nil {
				return osInfo, nil
			}
			ud.logger.Debug().Msgf("Processing %q failed: %s", path, err.Error())
		}
	}

	return nil, errors.Newf("failed to detect OS version, type and codeName from release files: %s", paths)
}

type UnixOSDetectorOption = func(ud *unixOSDetector)

// WithUnixOSReleasePaths allows injecting custom release file path locations for Unix OSDetector.
func WithUnixOSReleasePaths(paths map[string]string) UnixOSDetectorOption {
	return func(ud *unixOSDetector) {
		if paths != nil {
			ud.osReleaseFilePaths = paths
		}
	}
}

// WithUnixCheckSequence allows injecting the sequence for release path checks
func WithUnixCheckSequence(seq []string) UnixOSDetectorOption {
	return func(ud *unixOSDetector) {
		ud.fileCheckSequence = seq
	}
}

// WithUnixOSDetectorLogger allows injecting logger for the OSDetector
func WithUnixOSDetectorLogger(logger zerolog.Logger) UnixOSDetectorOption {
	return func(ud *unixOSDetector) {
		ud.logger = logger
	}
}

func NewUnixOSDetector(opts ...UnixOSDetectorOption) OSDetector {
	ud := &unixOSDetector{
		fileCheckSequence: []string{
			LSBReleaseFileName,
			OSReleaseFileName,
			RedhatReleaseFileName,
		},
		osReleaseFilePaths: map[string]string{
			LSBReleaseFileName:    EtcLSBReleasePath,
			OSReleaseFileName:     EtcOSReleasePath,
			RedhatReleaseFileName: EtcRedhatReleasePath,
		},
		logger: nolog,
	}

	for _, opt := range opts {
		opt(ud)
	}

	return ud
}
