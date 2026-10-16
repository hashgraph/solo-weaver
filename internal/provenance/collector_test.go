// SPDX-License-Identifier: Apache-2.0

package provenance

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestClusterProbe_SkippedForHostNeed: a host-only need never reads the cluster,
// so the cluster probe leaves the live fields untouched (no API probe at all).
func TestClusterProbe_SkippedForHostNeed(t *testing.T) {
	in := Input{}
	require.NoError(t, clusterProbe{}.collect(context.Background(), NeedHost, &in))
	require.False(t, in.APIReachable)
	require.Empty(t, in.LiveClusterID)
}

// TestRecordedIdentityProbe_Defaults: until #1235 lands nothing is recorded, so
// the provision record and recorded host identity stay empty.
func TestRecordedIdentityProbe_Defaults(t *testing.T) {
	in := Input{}
	require.NoError(t, recordedIdentityProbe{}.collect(context.Background(), NeedHostAPI, &in))
	require.False(t, in.RecordedMachineIDPresent)
	require.False(t, in.HasProvisionRecord)
	require.False(t, in.ExternalStateExists)
}

// TestNewCollector_FusesProbes: the default collector runs its probes and fuses
// their output into one Input, passing through the kubeconfig flag. Uses a host
// need so it does not touch the cluster.
func TestNewCollector_FusesProbes(t *testing.T) {
	out, err := NewCollector().Collect(context.Background(), NeedHost, true)
	require.NoError(t, err)
	require.True(t, out.KubeconfigGiven)
	require.False(t, out.APIReachable, "host need must not probe the cluster")
}
