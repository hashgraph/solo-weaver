// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package blocknode

import (
	"context"
	"errors"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
)

func newMinimalUpgradeHandler() *UpgradeHandler {
	return &UpgradeHandler{}
}

// deployedStateForUpgrade returns a deployed block-node state at chart version
// 0.30.0 with the given traffic-shaping decision recorded.
func deployedStateForUpgrade(trafficShapingEnabled bool) state.State {
	return state.State{
		StateRecord: state.StateRecord{
			BlockNodeState: state.BlockNodeState{
				ReleaseInfo: state.HelmReleaseInfo{
					Status:       release.StatusDeployed,
					ChartRef:     "oci://example.com/block-node",
					ChartVersion: "0.30.0",
				},
				Storage:               models.BlockNodeStorage{BasePath: "/mnt/storage"},
				TrafficShapingEnabled: trafficShapingEnabled,
			},
		},
	}
}

func upgradeInputs() models.UserInputs[models.BlockNodeInputs] {
	return models.UserInputs[models.BlockNodeInputs]{
		Custom: models.BlockNodeInputs{
			Namespace:    "block-node-ns",
			Release:      "block-node",
			Chart:        "oci://example.com/block-node",
			ChartVersion: "0.31.0",
			Storage:      models.BlockNodeStorage{BasePath: "/mnt/storage"},
			ReuseValues:  true,
		},
	}
}

// TestUpgrade_TrafficShapingEnabled_ReAssertsPlane verifies that an upgrade of a
// block node that had traffic shaping enabled at install re-asserts the full
// network plane (create-if-missing) ahead of the chart upgrade.
func TestUpgrade_TrafficShapingEnabled_ReAssertsPlane(t *testing.T) {
	h := newMinimalUpgradeHandler()

	wb, err := h.BuildWorkflow(deployedStateForUpgrade(true), upgradeInputs())
	require.NoError(t, err)

	ids := workflowStepIDs(t, wb)
	assert.Equal(t, withMediaCheck(
		steps.NetworkFirewallCreateStepId,
		steps.NetworkPolicyCreateStepId,
		steps.NftWeaverPersistStepId,
		steps.TcEgressPersistStepId,
		steps.TcIngressRecordStepId,
		steps.BlockNodeDaemonConfigStepId,
		steps.RestartDaemonServiceStepId,
		steps.UpgradeBlockNodeStepId,
	), ids)
}

// TestUpgrade_TrafficShapingDisabled_NoTeardown verifies that upgrading a block
// node that had traffic shaping disabled at install re-asserts only the
// self-gating host firewall and never emits teardown steps — a routine version
// bump must not disarm anything.
func TestUpgrade_TrafficShapingDisabled_NoTeardown(t *testing.T) {
	h := newMinimalUpgradeHandler()

	wb, err := h.BuildWorkflow(deployedStateForUpgrade(false), upgradeInputs())
	require.NoError(t, err)

	ids := workflowStepIDs(t, wb)
	assert.Equal(t, withMediaCheck(
		steps.NetworkFirewallCreateStepId,
		steps.UpgradeBlockNodeStepId,
	), ids)
}

func markedForShapingCleanup(enabled bool) state.State {
	st := deployedStateForUpgrade(enabled)
	st.BlockNodeState.TrafficShapingCleanupPending = true
	return st
}

// TestUpgrade_ShapingCleanup_RemovesLiveShaping covers a node where an older
// upgrade turned shaping on with no recorded decision: the shaping bundle is
// removed ahead of the chart upgrade, and the host firewall is left alone.
func TestUpgrade_ShapingCleanup_RemovesLiveShaping(t *testing.T) {
	h := newMinimalUpgradeHandler()
	h.bnPoliciesPresent = func() (bool, error) { return true, nil }

	wb, err := h.BuildWorkflow(markedForShapingCleanup(false), upgradeInputs())
	require.NoError(t, err)

	ids := workflowStepIDs(t, wb)
	assert.Equal(t, withMediaCheck(
		steps.NetworkFirewallCreateStepId,
		steps.NetworkPolicyDeleteAllStepId,
		steps.TcEgressTeardownStepId,
		steps.BlockNodeDaemonConfigStepId,
		steps.RestartDaemonServiceStepId,
		ShapingCleanupDoneStepId,
		steps.UpgradeBlockNodeStepId,
	), ids)
	assert.False(t, h.shapingCleanupDone, "the mark must stay until the teardown steps succeed")
}

// TestUpgrade_ShapingCleanupDoneStep_SetsFlag: the step marks the cleanup done
// only when it runs, which is after every teardown step succeeded.
func TestUpgrade_ShapingCleanupDoneStep_SetsFlag(t *testing.T) {
	h := newMinimalUpgradeHandler()
	stp, err := h.shapingCleanupDoneStep().Build()
	require.NoError(t, err)

	rpt := stp.Execute(context.Background())
	require.NoError(t, rpt.Error)
	assert.True(t, h.shapingCleanupDone)
}

// TestUpgrade_ShapingCleanup_NothingLive clears the mark without touching the
// host when no BN policies exist.
func TestUpgrade_ShapingCleanup_NothingLive(t *testing.T) {
	for _, tc := range []struct {
		name    string
		present func() (bool, error)
		enabled bool
		want    bool
	}{
		{name: "no BN policies", present: func() (bool, error) { return false, nil }, want: true},
		{name: "decision is enabled", present: func() (bool, error) { return true, nil }, enabled: true, want: true},
		{name: "registry unreadable keeps the mark", present: func() (bool, error) { return false, errors.New("boom") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newMinimalUpgradeHandler()
			h.bnPoliciesPresent = tc.present

			wb, err := h.BuildWorkflow(markedForShapingCleanup(tc.enabled), upgradeInputs())
			require.NoError(t, err)

			ids := workflowStepIDs(t, wb)
			assert.NotContains(t, ids, steps.NetworkPolicyDeleteAllStepId)
			assert.NotContains(t, ids, ShapingCleanupDoneStepId)
			assert.Equal(t, tc.want, h.shapingCleanupDone)
		})
	}
}

// TestUpgrade_LeaveScaledDown_TrailingScaleDown pins --no-scale-up on both
// upgrade branches. The scale-down has to follow the chart upgrade, which
// re-asserts the chart's replica default.
func TestUpgrade_LeaveScaledDown_TrailingScaleDown(t *testing.T) {
	h := newMinimalUpgradeHandler()

	for _, tc := range []struct {
		name         string
		resetStorage bool
		want         []string
	}{
		{
			name:         "plain_upgrade",
			resetStorage: false,
			want: []string{
				steps.NetworkFirewallCreateStepId,
				steps.UpgradeBlockNodeStepId,
				steps.ScaleDownAfterUpgradeStepId,
			},
		},
		{
			name:         "with_reset",
			resetStorage: true,
			want: []string{
				steps.NetworkFirewallCreateStepId,
				steps.PurgeBlockNodeStorageStepId,
				steps.UpgradeBlockNodeStepId,
				steps.ScaleDownAfterUpgradeStepId,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inputs := upgradeInputs()
			inputs.Custom.ResetStorage = tc.resetStorage
			inputs.Custom.LeaveScaledDown = true

			// Traffic shaping disabled keeps the network prefix to the single
			// self-gating firewall step, so the assertion stays about the tail.
			wb, err := h.BuildWorkflow(deployedStateForUpgrade(false), inputs)
			require.NoError(t, err)

			assert.Equal(t, withMediaCheck(tc.want...), workflowStepIDs(t, wb))
		})
	}
}

// TestUpgrade_PurgeStorage_RecreatesBeforeChartUpgrade pins the branch
// --purge-storage adds to upgrade: wipe the deployed directories, recreate the
// PVs/PVCs at the requested paths, then apply the chart. Moving a block node and
// bumping its version is one operation.
func TestUpgrade_PurgeStorage_RecreatesBeforeChartUpgrade(t *testing.T) {
	h := newMinimalUpgradeHandler()

	inputs := upgradeInputs()
	inputs.Custom.Storage = models.BlockNodeStorage{BasePath: "/mnt/new"}
	inputs.Custom.ResetStorage = true
	inputs.Custom.PurgeStorage = true

	wb, err := h.BuildWorkflow(deployedStateForUpgrade(false), inputs)
	require.NoError(t, err)

	assert.Equal(t, "block-node-upgrade-purge-storage", wb.Id())
	assert.Equal(t, withMediaCheck(
		steps.NetworkFirewallCreateStepId,
		steps.PurgeBlockNodeStorageStepId,
		steps.RecreateBlockNodeStorageStepId,
		steps.UpgradeBlockNodeStepId,
	), workflowStepIDs(t, wb))
}

// TestUpgrade_PathChangeWithoutPurgeIsRejected covers both non-purge branches.
// Before the storage plan was shared, upgrade applied no guard at all: it wiped
// the requested paths and then upgraded against PVs still bound to the deployed
// ones.
func TestUpgrade_PathChangeWithoutPurgeIsRejected(t *testing.T) {
	h := newMinimalUpgradeHandler()

	for _, tc := range []struct {
		name         string
		resetStorage bool
	}{
		{name: "with_reset", resetStorage: true},
		{name: "no_storage_flag", resetStorage: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inputs := upgradeInputs()
			inputs.Custom.Storage = models.BlockNodeStorage{BasePath: "/mnt/new"}
			inputs.Custom.ResetStorage = tc.resetStorage

			_, err := h.BuildWorkflow(deployedStateForUpgrade(false), inputs)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "storage paths have changed")
			assertResolutionMentions(t, err, "--purge-storage")
		})
	}
}

// TestUpgrade_UnchangedPathsKeepExistingWorkflowIds guards against the shared
// storage plan renaming or reordering the branches that existed before it.
func TestUpgrade_UnchangedPathsKeepExistingWorkflowIds(t *testing.T) {
	h := newMinimalUpgradeHandler()

	for _, tc := range []struct {
		name         string
		resetStorage bool
		workflowId   string
		want         []string
	}{
		{
			name:         "plain_upgrade",
			resetStorage: false,
			workflowId:   "block-node-upgrade",
			want: []string{
				steps.NetworkFirewallCreateStepId,
				steps.UpgradeBlockNodeStepId,
			},
		},
		{
			name:         "with_reset",
			resetStorage: true,
			workflowId:   "block-node-upgrade-with-reset",
			want: []string{
				steps.NetworkFirewallCreateStepId,
				steps.PurgeBlockNodeStorageStepId,
				steps.UpgradeBlockNodeStepId,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inputs := upgradeInputs()
			inputs.Custom.ResetStorage = tc.resetStorage

			wb, err := h.BuildWorkflow(deployedStateForUpgrade(false), inputs)
			require.NoError(t, err)

			assert.Equal(t, tc.workflowId, wb.Id())
			assert.Equal(t, withMediaCheck(tc.want...), workflowStepIDs(t, wb))
		})
	}
}

func stateForUpgradeWithStatus(status release.Status) state.State {
	st := deployedStateForUpgrade(false)
	st.BlockNodeState.ReleaseInfo.Status = status
	return st
}

// TestUpgrade_FailedRelease_RetriesWithoutForce covers the retry after a failed
// upgrade, to a newer version and to the failed revision's own version.
func TestUpgrade_FailedRelease_RetriesWithoutForce(t *testing.T) {
	for _, version := range []string{"0.31.0", "0.30.0"} {
		t.Run(version, func(t *testing.T) {
			inputs := upgradeInputs()
			inputs.Custom.ChartVersion = version

			wb, err := newMinimalUpgradeHandler().BuildWorkflow(stateForUpgradeWithStatus(release.StatusFailed), inputs)
			require.NoError(t, err)
			assert.Contains(t, workflowStepIDs(t, wb), steps.UpgradeBlockNodeStepId)
		})
	}
}

// TestUpgrade_DeployedRelease_SameVersionStillNeedsForce is the negative control
// for the retry exemption above.
func TestUpgrade_DeployedRelease_SameVersionStillNeedsForce(t *testing.T) {
	inputs := upgradeInputs()
	inputs.Custom.ChartVersion = "0.30.0"

	_, err := newMinimalUpgradeHandler().BuildWorkflow(deployedStateForUpgrade(false), inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already at chart version")
}

// TestUpgrade_FailedRelease_KeepsStorageGuard verifies planStorage still compares
// paths on a failed release instead of treating it as not deployed.
func TestUpgrade_FailedRelease_KeepsStorageGuard(t *testing.T) {
	inputs := upgradeInputs()
	inputs.Custom.Storage = models.BlockNodeStorage{BasePath: "/mnt/other-storage"}

	_, err := newMinimalUpgradeHandler().BuildWorkflow(stateForUpgradeWithStatus(release.StatusFailed), inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage paths have changed")
}

// TestUpgrade_PendingRelease_RefusedEvenWithForce verifies --force does not push
// an upgrade into a release Helm holds locked.
func TestUpgrade_PendingRelease_RefusedEvenWithForce(t *testing.T) {
	inputs := upgradeInputs()
	inputs.Common.Force = true

	_, err := newMinimalUpgradeHandler().BuildWorkflow(stateForUpgradeWithStatus(release.StatusPendingUpgrade), inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"pending-upgrade"`)
	assertResolutionMentions(t, err, "helm rollback")
}
