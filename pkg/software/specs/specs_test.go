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

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSoftwareName_String(t *testing.T) {
	req := require.New(t)
	var testCases = []struct {
		input string
	}{
		{
			input: "test",
		},
		{
			input: "",
		},
	}

	for _, test := range testCases {
		v := SoftwareName(test.input)
		req.Equal(test.input, v.String())
	}

}

func TestOSFlavor_String(t *testing.T) {
	req := require.New(t)
	var testCases = []struct {
		input string
	}{
		{
			input: "test",
		},
		{
			input: "",
		},
	}

	for _, test := range testCases {
		v := OSFlavor(test.input)
		req.Equal(test.input, v.String())
	}
}

func TestOSType_String(t *testing.T) {
	req := require.New(t)
	var testCases = []struct {
		input string
	}{
		{
			input: "test",
		},
		{
			input: "",
		},
	}

	for _, test := range testCases {
		v := OSType(test.input)
		req.Equal(test.input, v.String())
	}
}

func TestOSVersion_String(t *testing.T) {
	req := require.New(t)
	var testCases = []struct {
		input string
	}{
		{
			input: "test",
		},
		{
			input: "",
		},
	}

	for _, test := range testCases {
		v := OSVersion(test.input)
		req.Equal(test.input, v.String())
	}
}

func TestSoftwareDefinition_GetName(t *testing.T) {
	req := require.New(t)
	s := SoftwareDefinition{
		Optional:   false,
		Executable: SoftwareExecutableSpec{Name: "test"},
		Specs:      nil,
	}

	req.Equal(s.Executable.Name, s.GetName())
}

func TestSoftwareDefinition_GetSoftwareSpec(t *testing.T) {
	req := require.New(t)
	expected := SoftwareSpec{
		Installable:             false,
		Managed:                 false,
		DefaultVersion:          "",
		RelaxHashVerification:   false,
		DisableHashVerification: false,
		Versions:                nil,
	}

	name := SoftwareName("test")
	osType := OSType("test")
	osFlavor := OSFlavor("test")
	osVersion := OSVersion("test")

	s := SoftwareDefinition{
		Optional:   false,
		Executable: SoftwareExecutableSpec{},
		Specs: map[OSType]OSFlavorBasedSpec{
			osType: {osFlavor: map[OSVersion]SoftwareSpec{osVersion: expected}},
		},
	}

	spec, err := s.GetSoftwareSpec(name, osType, osFlavor, osVersion)
	req.NoError(err)
	req.Equal(expected, spec)

	spec, err = s.GetSoftwareSpec(name, "invalid", osFlavor, osVersion)
	req.Error(err)
	req.Equal(SoftwareSpec{}, spec)

	spec, err = s.GetSoftwareSpec(name, osType, "invalid", osVersion)
	req.Error(err)
	req.Equal(SoftwareSpec{}, spec)

	spec, err = s.GetSoftwareSpec(name, osType, osFlavor, "invalid")
	req.Error(err)
	req.Equal(SoftwareSpec{}, spec)
}

func TestSoftwareSpec_GetSoftwareVersionSpec(t *testing.T) {
	req := require.New(t)
	ss := SoftwareSpec{
		Installable:             false,
		Managed:                 false,
		DefaultVersion:          "",
		RelaxHashVerification:   false,
		DisableHashVerification: false,
		Versions: []SoftwareVersionSpec{
			{
				Version:        "0.0.0",
				PackageName:    "",
				PackageVersion: "",
				Sha256Hash:     "",
			},
		},
	}

	versionSpec, err := ss.GetSoftwareVersionSpec("0.0.0")
	req.NoError(err)
	req.Equal(ss.Versions[0], versionSpec)

	versionSpec, err = ss.GetSoftwareVersionSpec("0.0.1")
	req.Error(err)
	req.Equal(SoftwareVersionSpec{}, versionSpec)
}
