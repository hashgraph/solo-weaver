/*
 * Copyright (C) 2021-2023 Hedera Hashgraph, LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package jvm

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	assertions "github.com/stretchr/testify/require"
)

var (
	mockVersionInfo = javaVersionInfo{
		"99.99.99", map[string]javaVersion{
			"99.99.99": {
				DownloadKey: "jdk-99.99.99",
				SHA256:      "b3d8d1b0b1b2b3b4b5b6b7b8b9b0b1b2b3b4b5b6b7b8b9b0b1b2b3b4b5b6b7",
				RegistryTag: "99.99.99",
			},
		},
	}
)

func TestSupportedJavaVersions_Unsupported(t *testing.T) {
	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert := assertions.New(t)

	jvm, err := New()
	assert.NoError(err)
	assert.False(jvm.SupportedJavaVersion("1.8.0_292"))
	_, err = jvm.JavaVersionDownloadKey("1.8.0_292")
	assert.Error(err)
	_, err = jvm.JavaVersionSHA256("1.8.0_292")
	assert.Error(err)
	_, err = jvm.JavaVersionRegistryTag("1.8.0_292")
	assert.Error(err)
}

// top level mapping for java_version_info.json
type JsonJavaVersionInfo struct {
	DefaultVersion string `json:"default_version"`
	Versions       map[string]JsonVersionInfo
}

// mapping for each version in java_version_info.json
type JsonVersionInfo struct {
	DownloadKey string `json:"download_key"`
	Sha256      string
	RegistryTag string `json:"registry_tag"`
}

func TestSupportedJavaVersions_Supported(t *testing.T) {
	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert := assertions.New(t)

	javaVersionFile := "../../../../test/data/java_version_info.json"
	fileContent, err := os.Open(javaVersionFile)
	assert.NoError(err)
	defer fileContent.Close()
	byteResult, _ := io.ReadAll(fileContent)
	var jsonJavaVersionInfo JsonJavaVersionInfo

	err = json.Unmarshal([]byte(byteResult), &jsonJavaVersionInfo)
	assert.NoError(err)

	testCases := jsonJavaVersionInfo.Versions
	assert.Condition(func() bool { return len(testCases) > 0 })

	jvm, err := New()
	assert.NoError(err)
	assert.False(jvm.SupportedJavaVersion("1.8.0_292"))

	for expectedVersion, expectedVersionInfo := range testCases {
		assert.True(jvm.SupportedJavaVersion(expectedVersion))
		downloadKey, err := jvm.JavaVersionDownloadKey(expectedVersion)
		assert.NoError(err)
		assert.Equal(expectedVersionInfo.DownloadKey, downloadKey)
		sha256, err := jvm.JavaVersionSHA256(expectedVersion)
		assert.NoError(err)
		assert.Equal(expectedVersionInfo.Sha256, sha256)
		registryTag, err := jvm.JavaVersionRegistryTag(expectedVersion)
		assert.NoError(err)
		assert.Equal(expectedVersionInfo.RegistryTag, registryTag)
	}
}

func TestJVM_DefaultJavaVersion(t *testing.T) {
	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert := assertions.New(t)

	jvm, err := New()
	assert.NoError(err)

	defaultJavaVersion := jvm.DefaultJavaVersion()
	assert.Equal("21.0.1", defaultJavaVersion)
}

func TestJVM_DownloadURL(t *testing.T) {
	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert := assertions.New(t)

	jvm, err := New()
	assert.NoError(err)

	downloadURL, err := jvm.DownloadURL("17.0.2")
	assert.NoError(err)
	assert.Equal("https://download.java.net/java/GA/jdk17.0.2/dfd4a8d0985749f896bed50d7138ee7f/8/GPL/openjdk-17.0.2_linux-x64_bin.tar.gz", downloadURL)
}

func TestJVM_DownloadURLBadJavaVersion(t *testing.T) {
	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert := assertions.New(t)

	jvm, err := New()
	assert.NoError(err)

	_, err = jvm.DownloadURL("1.8.0_292")
	assert.Error(err)
}

func TestJVM_JavaVersionAvailable(t *testing.T) {
	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert := assertions.New(t)

	jvm, err := New()
	assert.NoError(err)

	available, err := jvm.JavaVersionAvailable("17.0.2")
	assert.NoError(err)
	assert.True(available)
}

func TestJVM_JavaVersionAvailableBadVersion(t *testing.T) {
	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert := assertions.New(t)

	jvm, err := New()
	assert.NoError(err)

	_, err = jvm.JavaVersionAvailable("1.8.0_292")
	assert.Error(err)
}

func TestJVM_JavaVersionAvailableMockedURLBadVersion(t *testing.T) {
	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert := assertions.New(t)

	jvm := &jvm{
		versionInfo:        mockVersionInfo,
		downloadURLPattern: "https://download.java.net/java/GA/jdk%s/%s/GPL/openjdk-%s_linux-x64_bin.tar.gz",
	}

	_, err := jvm.JavaVersionAvailable("99.99.99")
	assert.ErrorContains(err, "unknown Java version for version")
}

func TestJVM_JavaVersionAvailableMockedURLCallError(t *testing.T) {
	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert := assertions.New(t)

	jvm := &jvm{
		versionInfo:        mockVersionInfo,
		downloadURLPattern: "bad://download.java.net/java/GA/jdk%s/%s/GPL/openjdk-%s_linux-x64_bin.tar.gz",
	}

	_, err := jvm.JavaVersionAvailable("99.99.99")
	assert.ErrorContains(err, "invalid download URL for version")
}
