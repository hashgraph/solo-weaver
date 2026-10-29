// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package blocknode

import (
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
)

// assertResolutionMentions extracts the ErrPropertyResolution from the given
// errorx error and asserts it contains the given substring. The resolution is
// metadata-only — err.Error() does NOT include it — so we have to pull it out.
func assertResolutionMentions(t *testing.T, err error, want string) {
	t.Helper()
	raw, ok := errorx.ExtractProperty(err, models.ErrPropertyResolution)
	require.True(t, ok, "expected error to carry a resolution property")
	resolution, ok := raw.(string)
	require.True(t, ok, "expected resolution property to be a string, got %T", raw)
	assert.Contains(t, resolution, want)
}

// newMinimalReconfigureHandler returns a handler with only the fields used
// by BuildWorkflow (runtime is not accessed during workflow construction).
func newMinimalReconfigureHandler() *ReconfigureHandler {
	return &ReconfigureHandler{
		BaseHandler: bll.BaseHandler[models.BlockNodeInputs]{},
		runtime:     nil,
	}
}

// deployedBlockNodeState returns a state where the block node is deployed at
// the given storage base path.
func deployedBlockNodeState(basePath string) state.State {
	return state.State{
		StateRecord: state.StateRecord{
			BlockNodeState: state.BlockNodeState{
				ReleaseInfo: state.HelmReleaseInfo{
					Status: release.StatusDeployed,
				},
				Storage: models.BlockNodeStorage{
					BasePath: basePath,
				},
			},
		},
	}
}

// reconfigureInputs returns minimal valid UserInputs for a reconfigure. Traffic
// shaping is enabled — the common case where a reconfigure re-asserts the plane.
func reconfigureInputs(basePath string, resetStorage bool) models.UserInputs[models.BlockNodeInputs] {
	return reconfigureInputsWithFlags(basePath, resetStorage, false)
}

// reconfigureInputsWithFlags is the full-flag variant for tests that need to
// exercise --purge-storage paths. Traffic shaping is enabled.
func reconfigureInputsWithFlags(basePath string, resetStorage, purgeStorage bool) models.UserInputs[models.BlockNodeInputs] {
	ins := reconfigureInputsTS(basePath, true)
	ins.Custom.ResetStorage = resetStorage || purgeStorage
	ins.Custom.PurgeStorage = purgeStorage
	return ins
}

// reconfigureInputsTS builds reconfigure inputs with an explicit traffic-shaping
// target, for exercising the enable vs disable convergence branches.
func reconfigureInputsTS(basePath string, trafficShapingEnabled bool) models.UserInputs[models.BlockNodeInputs] {
	return models.UserInputs[models.BlockNodeInputs]{
		Custom: models.BlockNodeInputs{
			Namespace:             "block-node-ns",
			Release:               "block-node",
			Chart:                 "oci://example.com/block-node",
			ChartVersion:          "0.30.0",
			Storage:               models.BlockNodeStorage{BasePath: basePath},
			ReuseValues:           true,
			TrafficShapingEnabled: trafficShapingEnabled,
		},
	}
}

// enableNetworkPrefix is the network-plane step prefix the convergence assembler
// emits when traffic shaping is enabled and the host firewall is not being torn
// down (the default in these tests, where config.Host.Disabled is false).
var enableNetworkPrefix = []string{
	steps.NetworkFirewallCreateStepId,
	steps.NetworkPolicyCreateStepId,
	steps.NftWeaverPersistStepId,
	steps.TcEgressPersistStepId,
	steps.TcIngressRecordStepId,
	steps.BlockNodeDaemonConfigStepId,
	steps.RestartDaemonServiceStepId,
}

// withMediaCheck prepends the operator-model legacy guard and the warn-only
// storage media check, which together head every reconfigure and upgrade workflow
// ahead of the network-plane steps (#1319).
func withMediaCheck(ids ...string) []string {
	return append([]string{steps.PrecheckBlockNodeLegacyStepId, steps.CheckStorageMediaStepId}, ids...)
}

// workflowStepIDs builds the given WorkflowBuilder and returns the IDs of the
// top-level child steps in execution order.
func workflowStepIDs(t *testing.T, wb *automa.WorkflowBuilder) []string {
	t.Helper()
	stp, err := wb.Build()
	require.NoError(t, err)

	wf, ok := stp.(automa.Workflow)
	require.True(t, ok, "expected a Workflow, got %T", stp)

	ids := make([]string, 0, len(wf.Steps()))
	for _, s := range wf.Steps() {
		ids = append(ids, s.Id())
	}
	return ids
}

// TestBuildWorkflow_WithReset_PathsUnchanged_DataOnly verifies that
// --with-reset wipes data but leaves PVs/PVCs intact: purge → upgrade.
func TestBuildWorkflow_WithReset_PathsUnchanged_DataOnly(t *testing.T) {
	h := newMinimalReconfigureHandler()

	currentState := deployedBlockNodeState("/mnt/storage")
	inputs := reconfigureInputs("/mnt/storage", true)

	wb, err := h.BuildWorkflow(currentState, inputs)

	require.NoError(t, err)
	require.NotNil(t, wb)
	assert.Equal(t, "block-node-reconfigure-with-reset", wb.Id())

	ids := workflowStepIDs(t, wb)
	assert.Equal(t, append(withMediaCheck(enableNetworkPrefix...),
		steps.PurgeBlockNodeStorageStepId,
		steps.UpgradeBlockNodeStepId,
	), ids)
}

// TestBuildWorkflow_WithReset_PathsChanged_ReturnsError verifies that
// --with-reset alone is rejected when storage paths change; the operator
// must now use --purge-storage to delete and recreate PVs/PVCs.
func TestBuildWorkflow_WithReset_PathsChanged_ReturnsError(t *testing.T) {
	h := newMinimalReconfigureHandler()

	currentState := deployedBlockNodeState("/mnt/old")
	inputs := reconfigureInputs("/mnt/new", true)

	wb, err := h.BuildWorkflow(currentState, inputs)

	require.Error(t, err)
	assert.Nil(t, wb)
	assert.Contains(t, err.Error(), "storage paths have changed")
	assertResolutionMentions(t, err, "--purge-storage")
}

// TestBuildWorkflow_PurgeStorage_IncludesRecreateStep verifies that
// --purge-storage triggers the full purge → recreate → upgrade chain
// (which deletes PVs/PVCs, creates new ones at the new paths, then upgrades).
func TestBuildWorkflow_PurgeStorage_IncludesRecreateStep(t *testing.T) {
	h := newMinimalReconfigureHandler()

	for _, tc := range []struct {
		name    string
		oldBase string
		newBase string
	}{
		{name: "paths_changed", oldBase: "/mnt/old", newBase: "/mnt/new"},
		{name: "paths_unchanged", oldBase: "/mnt/storage", newBase: "/mnt/storage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			currentState := deployedBlockNodeState(tc.oldBase)
			inputs := reconfigureInputsWithFlags(tc.newBase, false, true)

			wb, err := h.BuildWorkflow(currentState, inputs)

			require.NoError(t, err)
			require.NotNil(t, wb)
			assert.Equal(t, "block-node-reconfigure-purge-storage", wb.Id())

			ids := workflowStepIDs(t, wb)
			assert.Equal(t, append(withMediaCheck(enableNetworkPrefix...),
				steps.PurgeBlockNodeStorageStepId,
				steps.RecreateBlockNodeStorageStepId,
				steps.UpgradeBlockNodeStepId,
			), ids)
		})
	}
}

// TestBuildWorkflow_NoReset_SamePathsUpgradeOnly verifies the default non-reset
// path: a single UpgradeBlockNode (HelmCapsule patch). There is no separate
// rollout-restart — the operator rolls the pod when the values hash changes (#1319).
func TestBuildWorkflow_NoReset_SamePathsUpgradeOnly(t *testing.T) {
	h := newMinimalReconfigureHandler()

	currentState := deployedBlockNodeState("/mnt/storage")
	inputs := reconfigureInputs("/mnt/storage", false)

	wb, err := h.BuildWorkflow(currentState, inputs)

	require.NoError(t, err)
	assert.Equal(t, "block-node-reconfigure", wb.Id())

	ids := workflowStepIDs(t, wb)
	assert.Equal(t, append(withMediaCheck(enableNetworkPrefix...),
		steps.UpgradeBlockNodeStepId,
	), ids)
}

// TestBuildWorkflow_TrafficShapingDisabled_TearsDown verifies that resolving
// traffic shaping to disabled emits the teardown branch (policy delete, tc
// teardown, daemon-config disable) instead of the enable steps.
func TestBuildWorkflow_TrafficShapingDisabled_TearsDown(t *testing.T) {
	h := newMinimalReconfigureHandler()

	currentState := deployedBlockNodeState("/mnt/storage")
	inputs := reconfigureInputsTS("/mnt/storage", false)

	wb, err := h.BuildWorkflow(currentState, inputs)
	require.NoError(t, err)

	ids := workflowStepIDs(t, wb)
	assert.Equal(t, withMediaCheck(
		steps.NetworkFirewallCreateStepId,
		steps.NetworkPolicyDeleteAllStepId,
		steps.TcEgressTeardownStepId,
		steps.BlockNodeDaemonConfigStepId,
		steps.RestartDaemonServiceStepId,
		steps.UpgradeBlockNodeStepId,
	), ids)
}

// TestBuildWorkflow_FirewallDisabled_DeletesTable verifies that when the resolved
// host-firewall config is disabled (config.Host.Disabled=true), the convergence
// assembler emits NetworkFirewallDelete instead of NetworkFirewallCreate.
func TestBuildWorkflow_FirewallDisabled_DeletesTable(t *testing.T) {
	config.OverrideHostConfig(models.HostConfig{Disabled: true})
	t.Cleanup(func() { config.OverrideHostConfig(models.HostConfig{}) })

	h := newMinimalReconfigureHandler()
	currentState := deployedBlockNodeState("/mnt/storage")
	inputs := reconfigureInputsTS("/mnt/storage", false)

	wb, err := h.BuildWorkflow(currentState, inputs)
	require.NoError(t, err)

	ids := workflowStepIDs(t, wb)
	require.GreaterOrEqual(t, len(ids), 3)
	assert.Equal(t, steps.PrecheckBlockNodeLegacyStepId, ids[0])
	assert.Equal(t, steps.CheckStorageMediaStepId, ids[1])
	assert.Equal(t, steps.NetworkFirewallDeleteStepId, ids[2],
		"host firewall disabled should emit the delete step first in the network plane")
}

// TestBuildWorkflow_NoReset_ChangedPathsReturnsError verifies that changing
// storage paths without --purge-storage is blocked with a clear error that
// points at the right flag.
func TestBuildWorkflow_NoReset_ChangedPathsReturnsError(t *testing.T) {
	h := newMinimalReconfigureHandler()

	currentState := deployedBlockNodeState("/mnt/old")
	inputs := reconfigureInputs("/mnt/new", false)

	wb, err := h.BuildWorkflow(currentState, inputs)

	require.Error(t, err)
	assert.Nil(t, wb)
	assert.Contains(t, err.Error(), "storage paths have changed")
	assertResolutionMentions(t, err, "--purge-storage")
}

// TestBuildWorkflow_NotInstalled_ReturnsError verifies the guard condition.
func TestBuildWorkflow_NotInstalled_ReturnsError(t *testing.T) {
	h := newMinimalReconfigureHandler()

	currentState := state.State{} // zero value → status is not StatusDeployed
	inputs := reconfigureInputs("/mnt/storage", false)

	wb, err := h.BuildWorkflow(currentState, inputs)

	require.Error(t, err)
	assert.Nil(t, wb)
	assert.Contains(t, err.Error(), "block node is not installed")
}

// TestBuildWorkflow_LeaveScaledDown_NoTrailingScaleDownStep verifies that under the
// operator model --no-scale-up adds NO trailing scale-down step across every
// reconfigure branch: scaling down is declarative (replicaCount: 0 in the applied
// values), so each branch ends in the HelmCapsule patch (UpgradeBlockNode) with no
// extra imperative scale-down the operator would revert (#1319). The branch
// workflow ids are unchanged except that --no-scale-up no longer renames them.
func TestBuildWorkflow_LeaveScaledDown_NoTrailingScaleDownStep(t *testing.T) {
	h := newMinimalReconfigureHandler()

	for _, tc := range []struct {
		name       string
		inputs     models.UserInputs[models.BlockNodeInputs]
		workflowId string
		tail       []string
	}{
		{
			name:       "purge_storage",
			inputs:     reconfigureInputsWithFlags("/mnt/new", false, true),
			workflowId: "block-node-reconfigure-purge-storage",
			tail: []string{
				steps.PurgeBlockNodeStorageStepId,
				steps.RecreateBlockNodeStorageStepId,
				steps.UpgradeBlockNodeStepId,
			},
		},
		{
			name:       "with_reset",
			inputs:     reconfigureInputs("/mnt/storage", true),
			workflowId: "block-node-reconfigure-with-reset",
			tail: []string{
				steps.PurgeBlockNodeStorageStepId,
				steps.UpgradeBlockNodeStepId,
			},
		},
		{
			name:       "no_reset",
			inputs:     reconfigureInputs("/mnt/storage", false),
			workflowId: "block-node-reconfigure",
			tail: []string{
				steps.UpgradeBlockNodeStepId,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inputs := tc.inputs
			inputs.Custom.LeaveScaledDown = true

			wb, err := h.BuildWorkflow(deployedBlockNodeState("/mnt/storage"), inputs)

			require.NoError(t, err)
			require.NotNil(t, wb)
			assert.Equal(t, tc.workflowId, wb.Id())
			assert.Equal(t, append(withMediaCheck(enableNetworkPrefix...), tc.tail...),
				workflowStepIDs(t, wb))
		})
	}
}

// TestBuildWorkflow_NoRestart_HasNoEffect verifies that --no-restart no longer
// changes the workflow under the operator model: any values change triggers the
// operator to reconcile (and roll) the release, so the flag is accepted but the
// workflow is the plain reconfigure with a single HelmCapsule patch (#1319).
func TestBuildWorkflow_NoRestart_HasNoEffect(t *testing.T) {
	h := newMinimalReconfigureHandler()

	inputs := reconfigureInputs("/mnt/storage", false)
	inputs.Custom.NoRestart = true
	inputs.Custom.LeaveScaledDown = true

	wb, err := h.BuildWorkflow(deployedBlockNodeState("/mnt/storage"), inputs)

	require.NoError(t, err)
	assert.Equal(t, "block-node-reconfigure", wb.Id())
	assert.Equal(t, append(withMediaCheck(enableNetworkPrefix...),
		steps.UpgradeBlockNodeStepId,
	), workflowStepIDs(t, wb))
}

// deployedBlockNodeStateAtVersion is deployedBlockNodeState with the chart version
// filled in. The bare helper leaves ReleaseInfo.ChartVersion empty, which is what
// keeps the chart-version guard out of the way of every other test in this file.
func deployedBlockNodeStateAtVersion(basePath, version string) state.State {
	st := deployedBlockNodeState(basePath)
	st.BlockNodeState.ReleaseInfo.ChartVersion = version
	return st
}

// deployedBlockNodeStateAtChart is the chart-ref counterpart. reconfigureInputs
// uses "oci://example.com/block-node", so pass that to model no change.
func deployedBlockNodeStateAtChart(basePath, chartRef string) state.State {
	st := deployedBlockNodeState(basePath)
	st.BlockNodeState.ReleaseInfo.ChartRef = chartRef
	return st
}

// TestReconfigure_RejectsChartRefChange is the chart half of the same guard. The
// chart ref has no flag on this command either, so only a config file can change
// it — and reconfigure does not even emit the chart-switch warning that upgrade
// logs, so an unnoticed repo switch underneath a running release is the failure
// mode being closed off here.
//
// This test builds currentState by hand, so it cannot see whether the real
// pipeline ever delivers a non-empty ChartRef to BuildWorkflow. It does not:
// rebuilding BlockNodeState from a Helm release drops the chart ref, which made
// this guard unreachable in practice until the reality checker was fixed to
// preserve it. TestRefreshState_PreservesChartRef in internal/reality is the test
// that covers that half; neither is sufficient alone.
func TestReconfigure_RejectsChartRefChange(t *testing.T) {
	h := newMinimalReconfigureHandler()

	inputs := reconfigureInputs("/mnt/fast-storage", false)
	inputs.Custom.Chart = "oci://registry.example.com/hiero/block-node"

	_, err := h.BuildWorkflow(
		deployedBlockNodeStateAtChart("/mnt/fast-storage", "oci://example.com/block-node"), inputs)

	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.IllegalArgument),
		"a rejected config value must classify as IllegalArgument, got %v", err)
	assert.Contains(t, err.Error(), "oci://example.com/block-node")
	assert.Contains(t, err.Error(), "oci://registry.example.com/hiero/block-node")
	assertResolutionMentions(t, err, "block node upgrade")
}

// TestReconfigure_ForceDoesNotBypassChartRefGuard mirrors the version guard: a
// flag must not be able to turn a reconfigure into an unwarned chart switch.
func TestReconfigure_ForceDoesNotBypassChartRefGuard(t *testing.T) {
	h := newMinimalReconfigureHandler()

	inputs := reconfigureInputs("/mnt/fast-storage", false)
	inputs.Custom.Chart = "oci://registry.example.com/hiero/block-node"
	inputs.Common.Force = true

	_, err := h.BuildWorkflow(
		deployedBlockNodeStateAtChart("/mnt/fast-storage", "oci://example.com/block-node"), inputs)

	require.Error(t, err)
	assertResolutionMentions(t, err, "block node upgrade")
}

// TestReconfigure_MatchingChartRefProceeds is the negative control for the chart
// half: the ordinary reconfigure, where the effective chart is the deployed one.
func TestReconfigure_MatchingChartRefProceeds(t *testing.T) {
	h := newMinimalReconfigureHandler()

	inputs := reconfigureInputs("/mnt/fast-storage", false)

	wb, err := h.BuildWorkflow(
		deployedBlockNodeStateAtChart("/mnt/fast-storage", inputs.Custom.Chart), inputs)

	require.NoError(t, err)
	require.NotNil(t, wb)
}

// TestReconfigure_RejectsChartVersionChange pins the guard that stops a
// reconfigure from turning into an unguarded upgrade. Reconfigure re-applies
// values at the deployed version and runs none of upgrade's semver checks, so a
// config file declaring a different version would otherwise move the release with
// no downgrade protection.
func TestReconfigure_RejectsChartVersionChange(t *testing.T) {
	h := newMinimalReconfigureHandler()

	inputs := reconfigureInputs("/mnt/fast-storage", false)
	inputs.Custom.ChartVersion = "0.31.0"

	_, err := h.BuildWorkflow(deployedBlockNodeStateAtVersion("/mnt/fast-storage", "0.30.0"), inputs)

	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.IllegalArgument),
		"a rejected config value must classify as IllegalArgument, got %v", err)
	assert.Contains(t, err.Error(), "0.30.0")
	assert.Contains(t, err.Error(), "0.31.0")
	assertResolutionMentions(t, err, "block node upgrade")
}

// TestReconfigure_RejectsChartVersionDowngrade covers the direction that matters
// most: upgrade refuses a downgrade outright, so reconfigure must not become the
// way around that check.
func TestReconfigure_RejectsChartVersionDowngrade(t *testing.T) {
	h := newMinimalReconfigureHandler()

	inputs := reconfigureInputs("/mnt/fast-storage", false)
	inputs.Custom.ChartVersion = "0.29.0"

	_, err := h.BuildWorkflow(deployedBlockNodeStateAtVersion("/mnt/fast-storage", "0.30.0"), inputs)

	require.Error(t, err)
	assertResolutionMentions(t, err, "block node upgrade")
}

// TestReconfigure_ForceDoesNotBypassChartVersionGuard verifies the guard is not
// force-bypassable, unlike the not-installed precondition above it. The remedy is
// a different command, so letting --force through would just reintroduce the
// unguarded upgrade path.
func TestReconfigure_ForceDoesNotBypassChartVersionGuard(t *testing.T) {
	h := newMinimalReconfigureHandler()

	inputs := reconfigureInputs("/mnt/fast-storage", false)
	inputs.Custom.ChartVersion = "0.31.0"
	inputs.Common.Force = true

	_, err := h.BuildWorkflow(deployedBlockNodeStateAtVersion("/mnt/fast-storage", "0.30.0"), inputs)

	require.Error(t, err)
	assertResolutionMentions(t, err, "block node upgrade")
}

// TestReconfigure_MatchingChartVersionProceeds is the negative control: the
// ordinary reconfigure, where the effective version is the deployed one, must not
// be caught by the guard.
func TestReconfigure_MatchingChartVersionProceeds(t *testing.T) {
	h := newMinimalReconfigureHandler()

	inputs := reconfigureInputs("/mnt/fast-storage", false)
	inputs.Custom.ChartVersion = "0.30.0"

	wb, err := h.BuildWorkflow(deployedBlockNodeStateAtVersion("/mnt/fast-storage", "0.30.0"), inputs)

	require.NoError(t, err)
	require.NotNil(t, wb)
}

// deployedBlockNodeStateWithStatus models a release Helm still knows about but
// that is not deployed — a failed or half-applied upgrade. The reality checker
// fills ReleaseInfo for these too, so the guards have to look at Status.
func deployedBlockNodeStateWithStatus(basePath, version string, status release.Status) state.State {
	st := deployedBlockNodeStateAtVersion(basePath, version)
	st.BlockNodeState.ReleaseInfo.Status = status
	return st
}

// TestReconfigure_FailedReleaseSkipsChartGuards covers the interaction between
// the guards and the state tier. setStateSources drops the state and reality
// tiers for any status other than deployed, so the effective chart version falls
// back to the compiled-in default while ReleaseInfo still reports the version
// Helm has on record. Comparing the two would abort `reconfigure --force` — the
// remedy the not-installed precondition in this same function points at.
func TestReconfigure_FailedReleaseSkipsChartGuards(t *testing.T) {
	h := newMinimalReconfigureHandler()

	inputs := reconfigureInputs("/mnt/fast-storage", false)
	inputs.Custom.ChartVersion = "0.44.3" // deps.BLOCK_NODE_VERSION, not the release's
	inputs.Common.Force = true

	wb, err := h.BuildWorkflow(
		deployedBlockNodeStateWithStatus("/mnt/fast-storage", "0.40.0", release.StatusFailed), inputs)

	require.NoError(t, err, "a failed release must stay reconfigurable with --force")
	require.NotNil(t, wb)
}

// deployedBlockNodeStateWithLiveSize records a bound live PV at the given size,
// the way the reality checker reads capacity back off the PV.
func deployedBlockNodeStateWithLiveSize(basePath, liveSize string) state.State {
	st := deployedBlockNodeState(basePath)
	st.BlockNodeState.Storage.LiveSize = liveSize
	return st
}

// TestReconfigure_RejectsStorageSizeChange pins the guard on storage sizes. Only
// the recreate path re-renders PVs/PVCs, so on every other branch a new size
// would be carried all the way to a successful report while the PVC kept its old
// capacity. The guard lives in planStorage, so reset and upgrade inherit it.
func TestReconfigure_RejectsStorageSizeChange(t *testing.T) {
	h := newMinimalReconfigureHandler()

	inputs := reconfigureInputs("/mnt/fast-storage", false)
	inputs.Custom.Storage.LiveSize = "500Gi"

	_, err := h.BuildWorkflow(deployedBlockNodeStateWithLiveSize("/mnt/fast-storage", "200Gi"), inputs)

	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.IllegalArgument),
		"a rejected config value must classify as IllegalArgument, got %v", err)
	assert.Contains(t, err.Error(), "liveSize")
	assertResolutionMentions(t, err, "--purge-storage")
}

// TestReconfigure_PurgeStorageAllowsSizeChange is the escape hatch the guard's
// resolution names: --purge-storage deletes and recreates the PVs, so it is the
// one branch that can act on a new size.
func TestReconfigure_PurgeStorageAllowsSizeChange(t *testing.T) {
	h := newMinimalReconfigureHandler()

	inputs := reconfigureInputsWithFlags("/mnt/fast-storage", false, true)
	inputs.Custom.Storage.LiveSize = "500Gi"

	wb, err := h.BuildWorkflow(deployedBlockNodeStateWithLiveSize("/mnt/fast-storage", "200Gi"), inputs)

	require.NoError(t, err)
	require.NotNil(t, wb)
}

// TestReconfigure_EquivalentStorageSizeProceeds keeps the guard off spellings
// that mean the same capacity: a PV reports what it was created with, so a file
// saying 20Gi against a PV recorded as 20480Mi is not a change.
func TestReconfigure_EquivalentStorageSizeProceeds(t *testing.T) {
	h := newMinimalReconfigureHandler()

	inputs := reconfigureInputs("/mnt/fast-storage", false)
	inputs.Custom.Storage.LiveSize = "20Gi"

	wb, err := h.BuildWorkflow(deployedBlockNodeStateWithLiveSize("/mnt/fast-storage", "20480Mi"), inputs)

	require.NoError(t, err)
	require.NotNil(t, wb)
}
