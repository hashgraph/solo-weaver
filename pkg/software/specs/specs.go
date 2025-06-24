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

package specs

import "github.com/cockroachdb/errors"

// SoftwareName defines the software executable name
// For example, `docker` for DockerCE
type SoftwareName string

// String returns the string value
func (s *SoftwareName) String() string {
	return string(*s)
}

// OSType defines os type name
type OSType string

// String returns the string value
func (o *OSType) String() string {
	return string(*o)
}

// OSFlavor defines os flavor name
type OSFlavor string

// String returns the string value
func (o *OSFlavor) String() string {
	return string(*o)
}

// OSVersion defines os version string
type OSVersion string

// String returns the string value
func (o *OSVersion) String() string {
	return string(*o)
}

// SoftwareDefinition defines the data model for software specification
type SoftwareDefinition struct {
	Optional   bool                   `yaml:"optional" json:"optional"`
	Executable SoftwareExecutableSpec `yaml:"executable" json:"executable"`
	Specs      OSTypeBasedSpec        `yaml:"specs" json:"specs"`
}

type SoftwareExecutableSpec struct {
	Name            SoftwareName           `yaml:"name" json:"name"`
	DefaultLocation string                 `yaml:"default_location" json:"default_location"`
	VersionInfo     VersionDetectionSpec   `yaml:"version_info" json:"version_info"`
	RequiredVersion VersionRequirementSpec `yaml:"required_version" json:"required_version"`
}

type VersionDetectionSpec struct {
	Arguments string `yaml:"arguments" json:"arguments"`
	Regex     string `yaml:"regex", json:"regex"`
}

type VersionRequirementSpec struct {
	Minimum string `yaml:"minimum" json:"minimum"`
	Maximum string `yaml:"maximum" json:"maximum"`
}

type OSTypeBasedSpec = map[OSType]OSFlavorBasedSpec

type OSFlavorBasedSpec = map[OSFlavor]OSVersionBasedSpec

type OSVersionBasedSpec = map[OSVersion]SoftwareSpec

type SoftwareSpec struct {
	Installable             bool   `yaml:"installable" json:"installable"`
	Managed                 bool   `yaml:"managed" json:"managed"`
	DefaultVersion          string `yaml:"default_version" json:"default_version"`
	RelaxHashVerification   bool   `yaml:"relax_hash_verification" json:"relax_hash_verification"`
	DisableHashVerification bool   `yaml:"disable_hash_verification" json:"disable_hash_verification"`
	Versions                []SoftwareVersionSpec
}

type SoftwareVersionSpec struct {
	Version        string `yaml:"version" json:"version"`
	PackageName    string `yaml:"package_name" json:"package_name"`
	PackageVersion string `yaml:"package_version" json:"packageVersion"`
	Sha256Hash     string `yaml:"sha256" json:"sha256"`
}

// GetName returns the software name.
// This is a helper function to retrieve software name from the definition.
func (sd *SoftwareDefinition) GetName() SoftwareName {
	return sd.Executable.Name
}

// GetSoftwareSpec returns the SoftwareSpec for the given program name and OS details
func (sd *SoftwareDefinition) GetSoftwareSpec(name SoftwareName, osType OSType, osFlavor OSFlavor, osVersion OSVersion) (SoftwareSpec, error) {
	var ret SoftwareSpec

	osSpec, found := sd.Specs[osType]
	if !found {
		return ret, errors.Newf("%q software definition is unavailable for OS Type %q", name, osType)
	}

	flavorSpec, found := osSpec[osFlavor]
	if !found {
		return ret, errors.Newf("%q software definition is unavailable for OS Flavor %q", name, osFlavor)
	}

	versionSpec, found := flavorSpec[osVersion]
	if !found {
		return ret, errors.Newf("%q software definition is unavailable for OS Version %q", name, osVersion)
	}

	return versionSpec, nil
}

// GetSoftwareVersionSpec returns SoftwareVersionSpec for the given program version
func (ss *SoftwareSpec) GetSoftwareVersionSpec(version string) (SoftwareVersionSpec, error) {
	ret := SoftwareVersionSpec{
		Version:        "",
		PackageName:    "",
		PackageVersion: "",
		Sha256Hash:     "",
	}

	for _, v := range ss.Versions {
		if v.Version == version {
			return v, nil
		}
	}

	return ret, errors.Newf("software version spec cannot be found for version %q", version)
}

// OverrideRelaxCheck overrides relax hash verification setting
func (sd *SoftwareDefinition) OverrideRelaxCheck(relax bool) {
	// override relax check based on the parameter
	for osType, osSpec := range sd.Specs {
		for osFlavor, flavorSpec := range osSpec {
			for osVersion, spec := range flavorSpec {
				spec.RelaxHashVerification = relax
				sd.Specs[osType][osFlavor][osVersion] = spec
			}
		}
	}
}
