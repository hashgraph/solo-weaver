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
	"github.com/stretchr/testify/require"
	"path/filepath"
	"runtime"
	"testing"
)

var testDataDir = "../../tmp/data"

func TestUnixOSDetector_Scan_Redhat_Release(t *testing.T) {
	req := require.New(t)
	ud := &unixOSDetector{}
	path := filepath.Join(testDataDir, EtcRedhatReleasePath)
	osInfo, err := ud.scanRedhatReleaseFile(path, RedhatVersionRegex)
	req.NoError(err)
	req.Equal(runtime.GOOS, osInfo.Type)
	req.Equal(runtime.GOARCH, osInfo.Architecture)
	req.Equal(OSFlavorLinuxRhel, osInfo.Flavor)
	req.Equal("8.7", osInfo.Version)
	req.Equal("Ootpa", osInfo.CodeName)

	osInfo, err = ud.scanRedhatReleaseFile(path, "INVALID_REGEX[")
	req.Error(err)
	req.Contains(err.Error(), "failed to parse release version regex")
	req.Nil(osInfo)

	osInfo, err = ud.scanRedhatReleaseFile(path+"invalid", RedhatVersionRegex)
	req.Error(err)
	req.Nil(osInfo)

	// incorrect file
	path = filepath.Join(testDataDir, EtcOSReleasePath)
	osInfo, err = ud.scanRedhatReleaseFile(path, RedhatVersionRegex)
	req.NoError(err)
	req.Equal(runtime.GOOS, osInfo.Type)
	req.Equal(runtime.GOARCH, osInfo.Architecture)
	req.Equal(OSFlavorLinuxRhel, osInfo.Flavor)
	req.Empty(osInfo.Version)
	req.Empty(osInfo.CodeName)

	// no codename Redhat release string
	path = filepath.Join(testDataDir, EtcRedhatReleasePath+"-no-codename")
	osInfo, err = ud.scanRedhatReleaseFile(path, RedhatVersionRegex)
	req.NoError(err)
	req.Equal(runtime.GOOS, osInfo.Type)
	req.Equal(runtime.GOARCH, osInfo.Architecture)
	req.Equal(OSFlavorLinuxRhel, osInfo.Flavor)
	req.Empty(osInfo.Version)
	req.Empty(osInfo.CodeName)

	// no version Redhat release string
	path = filepath.Join(testDataDir, EtcRedhatReleasePath+"-no-version")
	osInfo, err = ud.scanRedhatReleaseFile(path, RedhatVersionRegex)
	req.NoError(err)
	req.Equal(runtime.GOOS, osInfo.Type)
	req.Equal(runtime.GOARCH, osInfo.Architecture)
	req.Equal(OSFlavorLinuxRhel, osInfo.Flavor)
	req.Empty(osInfo.Version)
	req.Empty(osInfo.CodeName)
}

func TestUnixOSDetector_Scan_LSB_Release(t *testing.T) {
	req := require.New(t)
	ud := &unixOSDetector{}
	path := filepath.Join(testDataDir, EtcLSBReleasePath)
	osInfo, err := ud.scanLSBReleaseFile(path)
	req.NoError(err)
	req.Equal(runtime.GOOS, osInfo.Type)
	req.Equal(runtime.GOARCH, osInfo.Architecture)
	req.Equal(OSFlavorLinuxUbuntu, osInfo.Flavor)
	req.Equal("20.04", osInfo.Version)
	req.Equal("focal", osInfo.CodeName)

	// incorrect file
	path = filepath.Join(testDataDir, EtcRedhatReleasePath)
	osInfo, err = ud.scanLSBReleaseFile(path)
	req.NoError(err)
	req.Equal(runtime.GOOS, osInfo.Type)
	req.Equal(runtime.GOARCH, osInfo.Architecture)
	req.Empty(osInfo.Flavor)
	req.Empty(osInfo.Version)
	req.Empty(osInfo.CodeName)
}

func TestUnixOSDetector_Scan_OS_Release(t *testing.T) {
	req := require.New(t)
	ud := &unixOSDetector{}
	path := filepath.Join(testDataDir, EtcOSReleasePath)
	osInfo, err := ud.scanOSReleaseFile(path)
	req.NoError(err)
	req.Equal(runtime.GOOS, osInfo.Type)
	req.Equal(runtime.GOARCH, osInfo.Architecture)
	req.Equal(OSFlavorLinuxDebian, osInfo.Flavor)
	req.Equal("11", osInfo.Version)
	req.Equal("bullseye", osInfo.CodeName)

	// incorrect file
	path = filepath.Join(testDataDir, EtcRedhatReleasePath)
	osInfo, err = ud.scanOSReleaseFile(path)
	req.NoError(err)
	req.Equal(runtime.GOOS, osInfo.Type)
	req.Equal(runtime.GOARCH, osInfo.Architecture)
	req.Empty(osInfo.Flavor)
	req.Empty(osInfo.Version)
	req.Empty(osInfo.CodeName)

}

func TestUnixOSDetector_ScanOS(t *testing.T) {
	req := require.New(t)
	ud := NewUnixOSDetector(
		WithUnixOSReleasePaths(map[string]string{
			RedhatReleaseFileName: filepath.Join(testDataDir, EtcRedhatReleasePath),
			LSBReleaseFileName:    filepath.Join(testDataDir, EtcLSBReleasePath+"invalid"),
			OSReleaseFileName:     filepath.Join(testDataDir, EtcOSReleasePath+"invalid"),
		}),
		WithUnixCheckSequence([]string{
			LSBReleaseFileName,
			OSReleaseFileName,
			RedhatReleaseFileName,
		}),
		WithUnixOSDetectorLogger(nolog),
	)
	osInfo, err := ud.ScanOS()
	req.NoError(err)
	req.Equal(runtime.GOOS, osInfo.Type)
	req.Equal(runtime.GOARCH, osInfo.Architecture)
	req.Equal(OSFlavorLinuxRhel, osInfo.Flavor)
	req.Equal("8.7", osInfo.Version)
	req.Equal("Ootpa", osInfo.CodeName)

	ud = NewUnixOSDetector(
		WithUnixOSReleasePaths(map[string]string{
			RedhatReleaseFileName: filepath.Join(testDataDir, EtcRedhatReleasePath+"invalid"),
			LSBReleaseFileName:    filepath.Join(testDataDir, EtcLSBReleasePath),
			OSReleaseFileName:     filepath.Join(testDataDir, EtcOSReleasePath+"invalid"),
		}),
		WithUnixCheckSequence([]string{
			OSReleaseFileName,
			RedhatReleaseFileName,
			LSBReleaseFileName,
		}),
		WithUnixOSDetectorLogger(nolog),
	)
	osInfo, err = ud.ScanOS()
	req.NoError(err)
	req.Equal(runtime.GOOS, osInfo.Type)
	req.Equal(runtime.GOARCH, osInfo.Architecture)
	req.Equal(OSFlavorLinuxUbuntu, osInfo.Flavor)
	req.Equal("20.04", osInfo.Version)
	req.Equal("focal", osInfo.CodeName)

	ud = NewUnixOSDetector(
		WithUnixOSReleasePaths(map[string]string{
			RedhatReleaseFileName: filepath.Join(testDataDir, EtcRedhatReleasePath+"invalid"),
			LSBReleaseFileName:    filepath.Join(testDataDir, EtcLSBReleasePath+"invalid"),
			OSReleaseFileName:     filepath.Join(testDataDir, EtcOSReleasePath),
		}),
		WithUnixCheckSequence([]string{
			RedhatReleaseFileName,
			LSBReleaseFileName,
			OSReleaseFileName,
		}),
		WithUnixOSDetectorLogger(nolog),
	)
	osInfo, err = ud.ScanOS()
	req.NoError(err)
	req.Equal(runtime.GOOS, osInfo.Type)
	req.Equal(runtime.GOARCH, osInfo.Architecture)
	req.Equal(OSFlavorLinuxDebian, osInfo.Flavor)
	req.Equal("11", osInfo.Version)
	req.Equal("bullseye", osInfo.CodeName)
}
