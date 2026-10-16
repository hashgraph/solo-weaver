// SPDX-License-Identifier: Apache-2.0

package provenance

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAllowedOrigins(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []Origin
		wantErr bool
	}{
		{name: "empty means no restriction", in: "", want: nil},
		{name: "whitespace means no restriction", in: "  ", want: nil},
		{name: "provisioned only", in: "provisioned", want: []Origin{Provisioned}},
		{name: "external only", in: "external", want: []Origin{External}},
		{name: "both, spaced and cased", in: " Provisioned , EXTERNAL ", want: []Origin{Provisioned, External}},
		{name: "trailing comma ignored", in: "provisioned,", want: []Origin{Provisioned}},
		{name: "unknown token fails closed", in: "provisioned,root", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAllowedOrigins(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestOriginAllowed(t *testing.T) {
	// No restriction: everything the gate passed is allowed.
	require.True(t, OriginAllowed(nil, Provisioned))
	require.True(t, OriginAllowed(nil, External))

	// provisioned-only allowlist refuses external, permits provisioned.
	allow := []Origin{Provisioned}
	require.True(t, OriginAllowed(allow, Provisioned))
	require.False(t, OriginAllowed(allow, External))
}

// TestRestrictedError_CarriesContext keeps the refusal operator-friendly.
func TestRestrictedError_CarriesContext(t *testing.T) {
	err := RestrictedError([]Origin{Provisioned}, External)
	require.Error(t, err)
	require.Contains(t, err.Error(), "external")
}
