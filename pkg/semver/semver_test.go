// SPDX-License-Identifier: Apache-2.0

package semver

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestNewSemver(t *testing.T) {
	var testCases = []struct {
		input  string
		output Semver
		errMsg string
	}{
		{
			input: "",
			output: Semver{
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
			output: Semver{
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
			output: Semver{
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
			output: Semver{
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
			output: Semver{
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
			output: Semver{
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
			output: Semver{
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
			output: Semver{
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
			output: Semver{
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
			output: Semver{
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
			output: Semver{
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
			output: Semver{
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
			output: Semver{},
			errMsg: "failed to parse",
		},
		{
			input:  "1.b.3",
			output: Semver{},
			errMsg: "failed to parse",
		},
		{
			input:  "1.2.c",
			output: Semver{},
			errMsg: "failed to parse",
		},
		{
			input:  "INVALID",
			output: Semver{},
			errMsg: "failed to parse",
		},
	}

	for _, test := range testCases {
		v, err := NewSemver(test.input)
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

func TestSemver_LessThan(t *testing.T) {
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
		version1, err := NewSemver(test.v1)
		assert.NoError(t, err)

		version2, err := NewSemver(test.v2)
		assert.NoError(t, err)

		assert.Equal(t, test.output, version1.LessThan(version2))
		assert.NotEqual(t, test.output, version1.GreaterOrEqual(version2))
	}
}

func TestSemver_GreaterThan(t *testing.T) {
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
		version1, err := NewSemver(test.v1)
		assert.NoError(t, err)

		version2, err := NewSemver(test.v2)
		assert.NoError(t, err)

		val := version1.GreaterThan(version2)
		assert.Equalf(t, test.output, val, "%s is not greater than %s", test.v1, test.v2)
	}
}

func TestCheckSemverRequirements(t *testing.T) {
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
			errMsg: "failed to parse",
		},
		{
			v:      "0.0.1",
			min:    "INVALID",
			max:    "0.0.2",
			errMsg: "failed to parse",
		},
		{
			v:      "0.0.1",
			min:    "0.0.1",
			max:    "INVALID",
			errMsg: "failed to parse",
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
