// SPDX-License-Identifier: Apache-2.0

package provenance

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRuleIDs_UniqueAndNonEmpty guards the stable-id contract: every Rule in
// allRules has a non-empty, unique value. A copy-paste that duplicates a shipped
// id (for example a new rule accidentally reusing "A.4") fails here instead of
// silently shadowing a published id in logs, hints and the design doc.
func TestRuleIDs_UniqueAndNonEmpty(t *testing.T) {
	seen := make(map[Rule]bool, len(allRules))
	for _, r := range allRules {
		require.NotEmpty(t, string(r), "a rule id must not be empty")
		require.False(t, seen[r], "duplicate rule id %q", r)
		seen[r] = true
	}
}
