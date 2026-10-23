// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package workflows

import (
	"slices"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/require"
)

// TestKubernetesSetupWorkflow_RecordsTheVersionRightAfterInit: once kubeadm init
// succeeds the host has Kubernetes, so a later failure in the same workflow
// (Cilium, MetalLB, metrics, the health check) must not leave the version
// unrecorded. Otherwise the next command re-runs every boundary migration on
// the cluster this CLI just built.
func TestKubernetesSetupWorkflow_RecordsTheVersionRightAfterInit(t *testing.T) {
	stp, err := KubernetesSetupWorkflow(nil).Build()
	require.NoError(t, err)
	wf, ok := stp.(automa.Workflow)
	require.True(t, ok, "expected a Workflow, got %T", stp)

	var ids []string
	for _, s := range wf.Steps() {
		ids = append(ids, s.Id())
	}
	init := slices.Index(ids, "init-cluster")
	require.NotEqual(t, -1, init, "steps: %v", ids)
	require.Less(t, init+1, len(ids))
	require.Equal(t, "record-provisioner-version", ids[init+1], "steps: %v", ids)

	records := 0
	for _, id := range ids {
		if id == "record-provisioner-version" {
			records++
		}
	}
	require.Equal(t, 1, records, "the version is recorded once; steps: %v", ids)
}
