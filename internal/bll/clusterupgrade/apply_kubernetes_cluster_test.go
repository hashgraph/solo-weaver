//go:build require_cluster

// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWithCluster_ApplyKubernetesHop applies the Kubernetes hop KUBE_FROM ->
// KUBE_TO (KUBE_STEP: patch, minor or resume) to the live cluster, against the
// real state directory. Run it as root on a VM:
//
//	KUBE_STEP=patch KUBE_FROM=1.33.4 KUBE_TO=1.33.13 go test -tags require_cluster \
//	  -run '^TestWithCluster_ApplyKubernetesHop$' ./internal/bll/clusterupgrade/
func TestWithCluster_ApplyKubernetesHop(t *testing.T) {
	step, from, to := os.Getenv("KUBE_STEP"), os.Getenv("KUBE_FROM"), os.Getenv("KUBE_TO")
	if step == "" || from == "" || to == "" {
		t.Skip("set KUBE_STEP, KUBE_FROM and KUBE_TO")
	}

	ctx, src, locks, run := liveSourcesOnHost(t)

	require.NoError(t, ApplyKubernetesHop(ctx, src, locks, run,
		Hop{Kind: KindKubernetes, Step: HopStep(step), Component: "kubernetes", From: from, To: to}))
}
