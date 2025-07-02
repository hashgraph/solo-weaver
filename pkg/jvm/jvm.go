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
    _ "embed"
    "encoding/json"
    "fmt"
    "net/http"

    "github.com/cockroachdb/errors"
)

const (
    linuxOpenJDKDownloadURLPattern = "https://download.java.net/java/GA/jdk%s/%s/GPL/openjdk-%s_linux-x64_bin.tar.gz"
)

//go:embed java_version_info.json
var jsonJavaVersionInfo []byte

type jvm struct {
    versionInfo        javaVersionInfo
    downloadURLPattern string
}

// JVM is an interface for working with Java versions and JVMs.
type JVM interface {
    // DefaultJavaVersion returns the default supported Java version.
    DefaultJavaVersion() string
    // SupportedJavaVersion returns true if the given Java version is supported.
    SupportedJavaVersion(string) bool
    // JavaVersionDownloadKey returns the download key for the given Java version and returns an error if the version is not supported.
    JavaVersionDownloadKey(string) (string, error)
    // JavaVersionSHA256 returns the SHA256 for the given Java version and returns an error if the version is not supported.
    JavaVersionSHA256(string) (string, error)
    // JavaVersionRegistryTag returns the registry tag for the given Java version and returns an error if the version is not supported.
    JavaVersionRegistryTag(string) (string, error)
    // DownloadURL returns the download URL for the given Java version and returns an error if the version is not supported.
    DownloadURL(string) (string, error)
    // JavaVersionAvailable returns true if the given Java version is supported and available for download and will return an error if the validation fails.
    JavaVersionAvailable(string) (bool, error)
}

// javaVersionInfo is for internal use and is the top level mapping for java_version_info.json
type javaVersionInfo struct {
    DefaultVersion string `json:"default_version"`
    Versions       map[string]javaVersion
}

// javaVersion is for internal use and is the mapping for each version in java_version_info.json
type javaVersion struct {
    DownloadKey string `json:"download_key"`
    SHA256      string `json:"sha256"`
    RegistryTag string `json:"registry_tag"`
}

// New returns a new JVM instance.
func New() (JVM, error) {
    var versionInfo javaVersionInfo
    err := json.Unmarshal(jsonJavaVersionInfo, &versionInfo)
    if err != nil {
        return nil, errors.Wrap(err, "error unmarshalling JSON into javaVersionInfo object")
    }

    return &jvm{
        versionInfo:        versionInfo,
        downloadURLPattern: linuxOpenJDKDownloadURLPattern,
    }, nil
}

func (j *jvm) SupportedJavaVersion(version string) bool {
    if _, ok := j.versionInfo.Versions[version]; ok {
        return true
    }
    return false
}

func (j *jvm) DefaultJavaVersion() string {
    return j.versionInfo.DefaultVersion
}

func (j *jvm) JavaVersionDownloadKey(version string) (string, error) {
    if version, ok := j.versionInfo.Versions[version]; ok {
        return version.DownloadKey, nil
    }
    return "", errors.Newf("no download key found for version %version", version)
}

func (j *jvm) JavaVersionSHA256(version string) (string, error) {
    if version, ok := j.versionInfo.Versions[version]; ok {
        return version.SHA256, nil
    }
    return "", errors.Newf("no sha256 found for version %version", version)
}

func (j *jvm) JavaVersionRegistryTag(version string) (string, error) {
    if version, ok := j.versionInfo.Versions[version]; ok {
        return version.RegistryTag, nil
    }
    return "", errors.Newf("no registry tag found for version %version", version)
}

func (j *jvm) DownloadURL(version string) (string, error) {
    downloadKey, err := j.JavaVersionDownloadKey(version)
    if err != nil {
        return "", err
    }

    downloadURL := fmt.Sprintf(j.downloadURLPattern, version, downloadKey, version)
    return downloadURL, nil
}

func (j *jvm) JavaVersionAvailable(version string) (bool, error) {
    if !j.SupportedJavaVersion(version) {
        return false, errors.Newf("version %s is not supported", version)
    }

    downloadUrl, err := j.DownloadURL(version)
    if err != nil {
        return false, err
    }

    resp, err := http.Head(downloadUrl)
    // an error making the http call
    if err != nil {
        return false, errors.Newf("invalid download URL for version %s and download link: %s", version, downloadUrl)
    }
    defer resp.Body.Close()

    // expect a 200 status code for a valid download link
    if resp.StatusCode != http.StatusOK {
        return false, errors.Newf("unknown Java version for version %s, OpenJDK version not available, http status code: %d, download link: %s", version, resp.StatusCode, downloadUrl)
    }

    return true, nil
}
