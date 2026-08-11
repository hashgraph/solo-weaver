// SPDX-License-Identifier: Apache-2.0

package secret

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/pkg/models"
)

func Test_parseSetFlags_Valid(t *testing.T) {
	entries, err := parseSetFlags([]string{
		"PROMETHEUS_PASSWORD_PRIMARY=secret/data/grafana/alloy/prod/prometheus/primary#password",
		"LOKI_PASSWORD_PRIMARY=secret/data/loki/primary",
	})
	require.NoError(t, err)
	require.Len(t, entries, 2)

	assert.Equal(t, models.ESOSecretDataEntry{
		SecretKey: "PROMETHEUS_PASSWORD_PRIMARY",
		RemoteKey: "secret/data/grafana/alloy/prod/prometheus/primary",
		Property:  "password",
	}, entries[0])

	assert.Equal(t, models.ESOSecretDataEntry{
		SecretKey: "LOKI_PASSWORD_PRIMARY",
		RemoteKey: "secret/data/loki/primary",
		Property:  "", // no #field
	}, entries[1])
}

func Test_parseSetFlags_Malformed(t *testing.T) {
	cases := map[string]struct {
		input    string
		errorMsg string
	}{
		"empty list":        {"", "at least one --set"},
		"no equals":         {"PROMETHEUS_PASSWORD", "expected KEY=store/path[#field]"},
		"empty key":         {"=secret/data/foo", "expected KEY=store/path[#field]"},
		"empty path":        {"KEY=", "invalid store path"},
		"bad key char":      {"BAD KEY=secret/data/foo", "invalid secret key"},
		"path with space":   {"KEY=secret/data /foo", "invalid store path"},
		"path with newline": {"KEY=secret/data\nfoo", "invalid store path"},
		"path with quote":   {"KEY=secret/\"data\"/foo", "invalid store path"},
		"bad property char": {"KEY=secret/data/foo#bad prop", "invalid property"},
		"multiple hashes":   {"KEY=secret/data/foo#bad#prop", "invalid property"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var flags []string
			if tc.input != "" {
				flags = []string{tc.input}
			}
			_, err := parseSetFlags(flags)
			require.ErrorContains(t, err, tc.errorMsg)
		})
	}
}

func Test_parseSetFlags_DuplicateKey(t *testing.T) {
	_, err := parseSetFlags([]string{"FOO=secret/data/a", "FOO=secret/data/b"})
	require.ErrorContains(t, err, "duplicate secret key")
}

func Test_parseSetFlags_TrailingHashMeansNoProperty(t *testing.T) {
	entries, err := parseSetFlags([]string{"FOO=secret/data/a#"})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "secret/data/a", entries[0].RemoteKey)
	assert.Equal(t, "", entries[0].Property)
}
