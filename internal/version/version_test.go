/*
 * Copyright 2016-2022 Hedera Hashgraph, LLC
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

package version

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestNewVersion(t *testing.T) {
	var testCases = []struct {
		input  string
		output Version
		errMsg string
	}{
		{
			input: "",
			output: Version{
				raw:        "",
				major:      0,
				minor:      0,
				patch:      0,
				preRelease: "",
				build:      "",
			},
		},
		{
			input: "219 ",
			output: Version{
				raw:        "219",
				major:      219, // support just a number used by some linux software such as systemctl
				minor:      0,
				patch:      0,
				preRelease: "",
				build:      "",
			},
		},
		{
			input: "8.30",
			output: Version{
				raw:        "8.30",
				major:      8,
				minor:      30,
				patch:      0,
				preRelease: "",
				build:      "",
			},
		},
		{
			input: "v1", // support v prefix
			output: Version{
				raw:        "v1",
				major:      1,
				minor:      0,
				patch:      0,
				preRelease: "",
				build:      "",
			},
		},
		{
			input: "v1.1",
			output: Version{
				raw:        "v1.1",
				major:      1,
				minor:      1,
				patch:      0,
				preRelease: "",
				build:      "",
			},
		},
		{
			input: "v1.1.2",
			output: Version{
				raw:        "v1.1.2",
				major:      1,
				minor:      1,
				patch:      2,
				preRelease: "",
				build:      "",
			},
		},
		{
			input: "1.1.2",
			output: Version{
				raw:        "1.1.2",
				major:      1,
				minor:      1,
				patch:      2,
				preRelease: "",
				build:      "",
			},
		},
		{
			input: "1.1.2-alpha.1",
			output: Version{
				raw:        "1.1.2-alpha.1",
				major:      1,
				minor:      1,
				patch:      2,
				preRelease: "alpha.1",
				build:      "",
			},
		},
		{
			input: "v1.1.2-beta.1",
			output: Version{
				raw:        "v1.1.2-beta.1",
				major:      1,
				minor:      1,
				patch:      2,
				preRelease: "beta.1",
				build:      "",
			},
		},
		{
			input: "v1.1.2-rc.1",
			output: Version{
				raw:        "v1.1.2-rc.1",
				major:      1,
				minor:      1,
				patch:      2,
				preRelease: "rc.1",
				build:      "",
			},
		},
		{
			input: "v1.1.2-rc1",
			output: Version{
				raw:        "v1.1.2-rc1",
				major:      1,
				minor:      1,
				patch:      2,
				preRelease: "rc1",
				build:      "",
			},
		},
		{
			input: "v1.1.2-rc.1.3+abdc6",
			output: Version{
				raw:        "v1.1.2-rc.1.3+abdc6",
				major:      1,
				minor:      1,
				patch:      2,
				preRelease: "rc.1.3",
				build:      "abdc6",
			},
		},
		{
			input:  "a.2.3",
			output: Version{},
			errMsg: "failed to parse version",
		},
		{
			input:  "1.b.3",
			output: Version{},
			errMsg: "failed to parse version",
		},
		{
			input:  "1.2.c",
			output: Version{},
			errMsg: "failed to parse version",
		},
		{
			input:  "INVALID",
			output: Version{},
			errMsg: "failed to parse version",
		},
	}

	for _, test := range testCases {
		v, err := NewVersion(test.input)
		if test.errMsg != "" {
			assert.Error(t, err)
			assert.Contains(t, err.Error(), test.errMsg)
		} else {
			assert.NoError(t, err)
			assert.True(t, test.output.EqualTo(v))
			assert.Equal(t, strings.TrimSpace(test.input), v.Raw())
		}
	}
}

func TestVersion_LessThan(t *testing.T) {
	var testCases = []struct {
		v1     string
		v2     string
		output bool
	}{
		{
			v1:     "0.2.3",
			v2:     "0.2.3",
			output: false,
		},
		{
			v1:     "0.2.3",
			v2:     "1.2.3",
			output: true,
		},
		{
			v1:     "1.0.1",
			v2:     "0.0.1",
			output: false,
		},
		{
			v1:     "1.0.0",
			v2:     "1.1.1",
			output: true,
		},
		{
			v1:     "1.1.1",
			v2:     "1.0.1",
			output: false,
		},
		{
			v1:     "1.1.0",
			v2:     "1.1.1",
			output: true,
		},
		{
			v1:     "1.1.1",
			v2:     "1.1.0",
			output: false,
		},
		{
			v1:     "1.0.0-alpha.1",
			v2:     "1.0.0",
			output: true,
		},
		{
			v1:     "1.0.0",
			v2:     "1.0.0-alpha.1",
			output: false,
		},
		{
			v1:     "1.0.0-alpha.1",
			v2:     "1.0.0-alpha.2",
			output: true,
		},
		{
			v1:     "1.0.0-alpha.2",
			v2:     "1.0.0-alpha.1",
			output: false,
		},
		{
			v1:     "1.0.0-alpha.2",
			v2:     "1.0.0-beta.1",
			output: true,
		},
		{
			v1:     "1.0.0-alpha.2",
			v2:     "1.0.0-alpha.1.1",
			output: false,
		},
		{
			v1:     "1.0.0-alpha",
			v2:     "1.0.0-rc1",
			output: true,
		},
		{
			v1:     "1.0.0-rc2",
			v2:     "1.0.0-rc1",
			output: false,
		},
	}

	for _, test := range testCases {
		version1, err := NewVersion(test.v1)
		assert.NoError(t, err)

		version2, err := NewVersion(test.v2)
		assert.NoError(t, err)

		assert.Equal(t, test.output, version1.LessThan(version2))
		assert.NotEqual(t, test.output, version1.GreaterOrEqual(version2))
	}
}

func TestVersion_GreaterThan(t *testing.T) {
	var testCases = []struct {
		v1     string
		v2     string
		output bool
	}{
		{
			v1:     "0.2.3",
			v2:     "0.2.3",
			output: false,
		},
		{
			v1:     "0.2.3",
			v2:     "1.2.3",
			output: false,
		},
		{
			v1:     "1.0.1",
			v2:     "0.0.1",
			output: true,
		},
		{
			v1:     "1.0.0",
			v2:     "1.1.1",
			output: false,
		},
		{
			v1:     "1.1.1",
			v2:     "1.0.1",
			output: true,
		},
		{
			v1:     "1.1.0",
			v2:     "1.1.1",
			output: false,
		},
		{
			v1:     "1.1.1",
			v2:     "1.1.0",
			output: true,
		},
		{
			v1:     "1.0.0-alpha.1",
			v2:     "1.0.0",
			output: false,
		},
		{
			v1:     "1.0.0",
			v2:     "1.0.0-alpha.1",
			output: true,
		},
		{
			v1:     "1.0.0-alpha.1",
			v2:     "1.0.0-alpha.2",
			output: false,
		},
		{
			v1:     "1.0.0-alpha.2",
			v2:     "1.0.0-alpha.1",
			output: true,
		},
		{
			v1:     "1.0.0-alpha.2",
			v2:     "1.0.0-beta.1",
			output: false,
		},
		{
			v1:     "1.0.0-alpha.2",
			v2:     "1.0.0-alpha.1.1",
			output: true,
		},
		{
			v1:     "1.0.0-alpha",
			v2:     "1.0.0-rc1",
			output: false,
		},
		{
			v1:     "1.0.0-rc2",
			v2:     "1.0.0-rc1",
			output: true,
		},
	}

	for _, test := range testCases {
		version1, err := NewVersion(test.v1)
		assert.NoError(t, err)

		version2, err := NewVersion(test.v2)
		assert.NoError(t, err)

		val := version1.GreaterThan(version2)
		assert.Equalf(t, test.output, val, "%s is not greater than %s", test.v1, test.v2)
	}
}

func TestCheckVersionRequirements(t *testing.T) {
	var testCases = []struct {
		v      string
		min    string
		max    string
		errMsg string
	}{
		{
			v:      "",
			min:    "",
			max:    "",
			errMsg: "",
		},
		{
			v:      "0.0.1",
			min:    "0.0.0",
			max:    "0.0.2",
			errMsg: "",
		},
		{
			v:      "0.0.1",
			min:    "0.0.1",
			max:    "0.0.2",
			errMsg: "",
		},
		{
			v:      "0.0.2",
			min:    "0.0.1",
			max:    "0.0.2",
			errMsg: "",
		},
		{
			v:      "0.0.0",
			min:    "0.0.1",
			max:    "0.0.2",
			errMsg: "is less than minimum required version",
		},
		{
			v:      "0.0.3",
			min:    "0.0.1",
			max:    "0.0.2",
			errMsg: "is greater than maximum required version",
		},
		{
			v:      "INVALID",
			min:    "0.0.1",
			max:    "0.0.2",
			errMsg: "failed to parse program's version string",
		},
		{
			v:      "0.0.1",
			min:    "INVALID",
			max:    "0.0.2",
			errMsg: "failed to parse minimum version requirement",
		},
		{
			v:      "0.0.1",
			min:    "0.0.1",
			max:    "INVALID",
			errMsg: "failed to parse maximum version requirement",
		},
	}

	req := require.New(t)
	for _, test := range testCases {
		err := CheckVersionRequirements(test.v, test.min, test.max)
		if test.errMsg != "" {
			req.Error(err)
			req.Contains(err.Error(), test.errMsg)
		} else {
			req.NoError(err)
		}

	}
}
