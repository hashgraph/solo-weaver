// SPDX-License-Identifier: Apache-2.0

package secret

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_normalizeRefreshInterval_Valid(t *testing.T) {
	cases := map[string]struct {
		input string
		want  string
	}{
		"hours":            {"1h", "1h0m0s"},
		"minutes":          {"30m", "30m0s"},
		"compound":         {"2h30m", "2h30m0s"},
		"minutes overflow": {"90m", "1h30m0s"},
		"fractional hours": {"1.5h", "1h30m0s"},
		"seconds":          {"10s", "10s"},
		"sub-second":       {"500ms", "500ms"},
		"zero":             {"0", "0s"}, // ESO "fetch once" semantics
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := normalizeRefreshInterval(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func Test_normalizeRefreshInterval_Invalid(t *testing.T) {
	cases := map[string]string{
		"empty":               "",
		"not a duration":      "banana",
		"bad unit":            "1x",
		"negative":            "-1h",
		"number without unit": "15",
		"newline injection":   "1h\n  deletionPolicy: Delete",
	}

	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := normalizeRefreshInterval(input)
			require.ErrorContains(t, err, "invalid --refresh-interval")
		})
	}
}
