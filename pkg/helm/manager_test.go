// SPDX-License-Identifier: Apache-2.0

package helm

import (
	"testing"

	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/action"
)

// TestApplyReuseValues asserts the contract that UpgradeChart maps our
// "reuse previously supplied user values" intent onto Helm's
// ResetThenReuseValues field, never onto the older ReuseValues field.
//
// Helm's plain ReuseValues=true overwrites the new chart's values.yaml
// defaults with the old release's coalesced values, silently dropping any
// key the new chart added — surfacing as nil-pointer template errors when
// crossing a chart version that introduces a new default key (#633).
//
// If anyone ever flips client.ReuseValues=true again, this test fails.
func TestApplyReuseValues(t *testing.T) {
	tests := []struct {
		name          string
		reuseValues   bool
		hasValueOpts  bool
		wantResetThen bool
	}{
		{
			name:          "reuse=true with new values: keep old user values, reset chart defaults to new chart",
			reuseValues:   true,
			hasValueOpts:  true,
			wantResetThen: true,
		},
		{
			name:          "reuse=false with new values: discard old user values entirely",
			reuseValues:   false,
			hasValueOpts:  true,
			wantResetThen: false,
		},
		{
			name:          "no new values, reuse=true: force reset-then-reuse so new chart defaults still apply",
			reuseValues:   true,
			hasValueOpts:  false,
			wantResetThen: true,
		},
		{
			name:          "no new values, reuse=false: still force reset-then-reuse (no other source of values)",
			reuseValues:   false,
			hasValueOpts:  false,
			wantResetThen: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &action.Upgrade{}
			applyReuseValues(client, tc.reuseValues, tc.hasValueOpts)

			require.Equal(t, tc.wantResetThen, client.ResetThenReuseValues,
				"ResetThenReuseValues should be %v", tc.wantResetThen)
			require.False(t, client.ReuseValues,
				"client.ReuseValues must never be set — it discards the new chart's values.yaml defaults (see #633)")
		})
	}
}
