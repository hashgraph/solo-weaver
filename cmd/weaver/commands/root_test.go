// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"testing"

	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/stretchr/testify/require"
)

func TestNoShortNameCollisionsInRealCommandTree(t *testing.T) {
	require.False(t, common.DetectShortNameCollisions(rootCmd),
		"short name collisions detected in command tree")
}
