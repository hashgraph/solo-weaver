// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package blocknode

import (
	"testing"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMinimalInstallHandler returns a handler with only the fields used by
// BuildWorkflow (runtime and mr are not accessed during workflow construction),
// owning managed.
func newMinimalInstallHandler(managed ...bll.Component) *InstallHandler {
	return &InstallHandler{BaseHandler: bll.BaseHandler[models.BlockNodeInputs]{Managed: managed}}
}

// installInputs returns minimal valid UserInputs for an install. Traffic
// shaping is off by default so the daemon-config step is excluded.
func installInputs(basePath string, trafficShapingEnabled bool) models.UserInputs[models.BlockNodeInputs] {
	return models.UserInputs[models.BlockNodeInputs]{
		Custom: models.BlockNodeInputs{
			Namespace:             "block-node-ns",
			Release:               "block-node",
			Chart:                 "oci://example.com/block-node",
			ChartVersion:          "0.37.0",
			Storage:               models.BlockNodeStorage{BasePath: basePath},
			TrafficShapingEnabled: trafficShapingEnabled,
		},
	}
}

// TestInstallHandler_BuildWorkflow_ExistingCluster_MediaCheckRunsEarly covers
// the branch taken when kube cluster install already ran: EnsureWeaverOwnerStep
// (the backwards-compat guard), then the warn-only storage media check "before
// the long install so the operator sees it early" (install_handler.go), then
// the network plane and the block-node setup itself.
func TestInstallHandler_BuildWorkflow_ExistingCluster_MediaCheckRunsEarly(t *testing.T) {
	h := newMinimalInstallHandler()
	currentState := state.State{StateRecord: state.StateRecord{
		ClusterState: state.ClusterState{Created: true},
	}}

	wb, err := h.BuildWorkflow(currentState, installInputs("/mnt/storage", true))
	require.NoError(t, err)
	require.NotNil(t, wb)

	ids := workflowStepIDs(t, wb)
	require.GreaterOrEqual(t, len(ids), 2)
	assert.Equal(t, "ensure-weaver-owner", ids[0])
	assert.Equal(t, steps.CheckStorageMediaStepId, ids[1],
		"storage media check must run right after the weaver-owner guard, ahead of the long install")
	assert.Equal(t, []string{
		"ensure-weaver-owner",
		steps.CheckStorageMediaStepId,
		"block-node-network-setup",
		steps.SetupBlockNodeStepId,
		"block-node-daemon-config",
	}, ids)
}

// TestInstallHandler_BuildWorkflow_FreshInstall_MediaCheckRunsEarly covers the
// branch taken when no cluster exists yet: the same weaver-owner guard and
// storage media check run before NodeSetupWorkflow/KubernetesSetupWorkflow
// stand up the substrate, so the operator sees a non-local storage warning
// before the (potentially long) cluster bootstrap even starts.
func TestInstallHandler_BuildWorkflow_FreshInstall_MediaCheckRunsEarly(t *testing.T) {
	h := newMinimalInstallHandler(bll.BlockNode, bll.Profile, bll.Firewall, bll.System)
	currentState := state.State{StateRecord: state.StateRecord{
		ClusterState: state.ClusterState{Created: false},
	}}

	wb, err := h.BuildWorkflow(currentState, installInputs("/mnt/storage", false))
	require.NoError(t, err)
	require.NotNil(t, wb)

	ids := workflowStepIDs(t, wb)
	require.GreaterOrEqual(t, len(ids), 2)
	assert.Equal(t, "ensure-weaver-owner", ids[0])
	assert.Equal(t, steps.CheckStorageMediaStepId, ids[1],
		"storage media check must run right after the weaver-owner guard, ahead of cluster bootstrap")
	assert.Equal(t, []string{
		"ensure-weaver-owner",
		steps.CheckStorageMediaStepId,
		"block-node-setup",
		"kubernetes-setup",
		"block-node-network-setup",
		steps.SetupBlockNodeStepId,
	}, ids, "traffic shaping disabled: no daemon-config step")
}

// TestInstallHandler_BuildWorkflow_NoClusterWithoutSystemIsRefused: standing up
// Kubernetes writes the system record, so the install handler that does not
// own it, picked because Kubernetes was installed when the command started,
// refuses instead of guessing.
func TestInstallHandler_BuildWorkflow_NoClusterWithoutSystemIsRefused(t *testing.T) {
	h := newMinimalInstallHandler(bll.BlockNode, bll.Profile, bll.Firewall)
	noCluster := state.State{StateRecord: state.StateRecord{ClusterState: state.ClusterState{Created: false}}}

	wb, err := h.BuildWorkflow(noCluster, installInputs("/mnt/storage", false))
	require.Nil(t, wb)
	require.ErrorContains(t, err,
		"found no running Kubernetes cluster, although Kubernetes was installed when this command started")
	reason, ok := errx.ReasonOf(err)
	require.True(t, ok, "no reason attached")
	require.Equal(t, reasons.PreconditionNotMet, reason)
	hints, ok := errx.Hints(err)
	require.True(t, ok, "no hints attached")
	require.Len(t, hints, 2)
	require.Contains(t, hints[0], "systemctl status kubelet")
	require.Contains(t, hints[1], "Re-run this command")
}
