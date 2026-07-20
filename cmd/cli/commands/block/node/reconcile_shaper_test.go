// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package node

import (
	"testing"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/stretchr/testify/require"
)

// TestReconcileShaperCmdSkipsGlobalChecks asserts that `block node
// reconcile-shaper` opts out of the installation check and startup
// migrations. Both read root-owned state (see common.RunPersistentPreRun),
// which would defeat --check's entire point of running unprivileged; the
// root.go superuser gate independently still requires root for the apply
// path. Without this annotation, --check fails for a non-root caller with a
// permission-denied reading /opt/solo/weaver/state/state.yaml rather than
// ever reaching the reconcile logic.
func TestReconcileShaperCmdSkipsGlobalChecks(t *testing.T) {
	require.False(t, common.RequireGlobalChecks(reconcileShaperCmd),
		"block node reconcile-shaper must opt out of global pre-run checks")
}
