//go:build require_cluster

// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWithCluster_ApplyCiliumHop applies the Cilium hop CILIUM_FROM ->
// CILIUM_TO to the live cluster, against the real state directory. Run it as
// root on a VM:
//
//	CILIUM_FROM=1.18.1 CILIUM_TO=1.18.16 go test -tags require_cluster \
//	  -run '^TestWithCluster_ApplyCiliumHop$' ./internal/bll/clusterupgrade/
func TestWithCluster_ApplyCiliumHop(t *testing.T) {
	from, to := os.Getenv("CILIUM_FROM"), os.Getenv("CILIUM_TO")
	if from == "" || to == "" {
		t.Skip("set CILIUM_FROM and CILIUM_TO")
	}
	f, err := parseVersion(from)
	require.NoError(t, err)
	tv, err := parseVersion(to)
	require.NoError(t, err)
	step := StepMinor
	if f.minorOf() == tv.minorOf() {
		step = StepPatch
	}

	ctx, src, locks, run := liveSourcesOnHost(t)

	require.NoError(t, ApplyCiliumHop(ctx, src, locks, run,
		Hop{Kind: KindCilium, Step: step, Component: "cilium", From: from, To: to}))
}
