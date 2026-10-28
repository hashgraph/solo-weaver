// SPDX-License-Identifier: Apache-2.0

package state

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// emptyWarnings empties the warning list now and when the test ends.
func emptyWarnings(t *testing.T) {
	t.Helper()
	TakeWarnings()
	t.Cleanup(func() { TakeWarnings() })
}

func TestAddWarning_SkipsDuplicates(t *testing.T) {
	emptyWarnings(t)

	addWarning("a")
	addWarning("b")
	addWarning("a")

	require.Equal(t, []string{"a", "b"}, TakeWarnings())
	require.Empty(t, TakeWarnings())
}
