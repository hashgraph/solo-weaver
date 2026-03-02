// SPDX-License-Identifier: Apache-2.0

package blocknode

// handler_test.go tests the Handler routing and per-action BuildWorkflow
// precondition guards without requiring a real rsl.Registry, Kubernetes cluster,
// or Helm chart.
//
// Strategy:
//   - handlerFor routing: call the unexported method directly and assert the
//     correct concrete type is returned for each action.
//   - BuildWorkflow preconditions: construct the handler directly, call
//     BuildWorkflow with controlled state snapshots, and assert that expected
//     errors are returned.
//   - PrepareEffectiveInputs pass-through: for Reset and Uninstall (which do
//     not resolve fields) verify that inputs are returned unchanged.

import (
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"helm.sh/helm/v3/pkg/release"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func deployedNodeState() state.BlockNodeState {
	return state.BlockNodeState{
		ReleaseInfo: state.HelmReleaseInfo{
			Status:   release.StatusDeployed,
			Version:  "0.22.1",
			ChartRef: "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server",
		},
	}
}

func notDeployedNodeState() state.BlockNodeState {
	return state.BlockNodeState{
		ReleaseInfo: state.HelmReleaseInfo{
			Status: release.StatusUninstalled,
		},
	}
}

func emptyClusterState() state.ClusterState { return state.ClusterState{} }

func createdClusterState() state.ClusterState { return state.ClusterState{Created: true} }

func defaultUserInputs() *models.UserInputs[models.BlocknodeInputs] {
	return &models.UserInputs[models.BlocknodeInputs]{
		Common: models.CommonInputs{
			ExecutionOptions: models.WorkflowExecutionOptions{
				ExecutionMode: automa.StopOnError,
				RollbackMode:  automa.StopOnError,
			},
		},
		Custom: models.BlocknodeInputs{
			Profile:      "local",
			Namespace:    "block-node-ns",
			Release:      "block-node",
			Version:      "0.22.1",
			Chart:        "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server",
			ChartVersion: "0.22.1",
		},
	}
}

// ── handlerFor routing ────────────────────────────────────────────────────────

func TestHandlerFor_ReturnsCorrectHandlerType(t *testing.T) {
	h := &Handler{
		install:   &InstallHandler{},
		upgrade:   &UpgradeHandler{},
		reset:     &ResetHandler{},
		uninstall: &UninstallHandler{},
	}

	cases := []struct {
		action  models.ActionType
		wantNil bool
		name    string
	}{
		{models.ActionInstall, false, "install"},
		{models.ActionUpgrade, false, "upgrade"},
		{models.ActionReset, false, "reset"},
		{models.ActionUninstall, false, "uninstall"},
		{"unknown-action", true, "unknown"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := h.handlerFor(tc.action)
			if tc.wantNil {
				if err == nil {
					t.Fatal("expected error for unknown action, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil {
				t.Fatal("expected non-nil handler")
			}
		})
	}
}

// ── InstallHandler.BuildWorkflow ──────────────────────────────────────────────

func TestInstallHandler_BuildWorkflow_AlreadyDeployed_WithoutForce_Errors(t *testing.T) {
	h := newInstallHandler(nil, nil) // rslAccessor not called by BuildWorkflow
	inputs := defaultUserInputs()

	_, err := h.BuildWorkflow(deployedNodeState(), emptyClusterState(), inputs)
	if err == nil {
		t.Fatal("expected error when block node is already deployed without --force")
	}
}

func TestInstallHandler_BuildWorkflow_AlreadyDeployed_WithForce_Succeeds(t *testing.T) {
	h := newInstallHandler(nil, nil)
	inputs := defaultUserInputs()
	inputs.Common.Force = true

	wb, err := h.BuildWorkflow(deployedNodeState(), emptyClusterState(), inputs)
	if err != nil {
		t.Fatalf("unexpected error with --force: %v", err)
	}
	if wb == nil {
		t.Fatal("expected non-nil WorkflowBuilder")
	}
}

func TestInstallHandler_BuildWorkflow_NotDeployed_ClusterExists_Succeeds(t *testing.T) {
	h := newInstallHandler(nil, nil)
	wb, err := h.BuildWorkflow(notDeployedNodeState(), createdClusterState(), defaultUserInputs())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wb == nil {
		t.Fatal("expected non-nil WorkflowBuilder")
	}
}

func TestInstallHandler_BuildWorkflow_NotDeployed_ClusterNotExists_Succeeds(t *testing.T) {
	h := newInstallHandler(nil, nil)
	wb, err := h.BuildWorkflow(notDeployedNodeState(), emptyClusterState(), defaultUserInputs())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wb == nil {
		t.Fatal("expected non-nil WorkflowBuilder")
	}
}

// ── UpgradeHandler.BuildWorkflow ──────────────────────────────────────────────

func TestUpgradeHandler_BuildWorkflow_NotDeployed_Errors(t *testing.T) {
	h := newUpgradeHandler(nil)
	_, err := h.BuildWorkflow(notDeployedNodeState(), emptyClusterState(), defaultUserInputs())
	if err == nil {
		t.Fatal("expected error when block node is not deployed")
	}
}

func TestUpgradeHandler_BuildWorkflow_ChartMismatch_Errors(t *testing.T) {
	h := newUpgradeHandler(nil)
	nodeState := deployedNodeState()
	nodeState.ReleaseInfo.ChartRef = "oci://different-chart/repo"

	_, err := h.BuildWorkflow(nodeState, emptyClusterState(), defaultUserInputs())
	if err == nil {
		t.Fatal("expected error when chart ref does not match")
	}
}

func TestUpgradeHandler_BuildWorkflow_Downgrade_Errors(t *testing.T) {
	h := newUpgradeHandler(nil)
	nodeState := deployedNodeState()
	nodeState.ReleaseInfo.Version = "1.0.0" // currently at 1.0.0

	inputs := defaultUserInputs()
	inputs.Custom.Version = "0.22.1" // trying to downgrade

	_, err := h.BuildWorkflow(nodeState, emptyClusterState(), inputs)
	if err == nil {
		t.Fatal("expected error for version downgrade")
	}
}

func TestUpgradeHandler_BuildWorkflow_SameVersion_WithoutForce_Errors(t *testing.T) {
	h := newUpgradeHandler(nil)
	_, err := h.BuildWorkflow(deployedNodeState(), emptyClusterState(), defaultUserInputs())
	if err == nil {
		t.Fatal("expected error when version is already at desired without --force")
	}
}

func TestUpgradeHandler_BuildWorkflow_SameVersion_WithForce_Succeeds(t *testing.T) {
	h := newUpgradeHandler(nil)
	inputs := defaultUserInputs()
	inputs.Common.Force = true

	wb, err := h.BuildWorkflow(deployedNodeState(), emptyClusterState(), inputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wb == nil {
		t.Fatal("expected non-nil WorkflowBuilder")
	}
}

func TestUpgradeHandler_BuildWorkflow_NewerVersion_Succeeds(t *testing.T) {
	h := newUpgradeHandler(nil)
	inputs := defaultUserInputs()
	inputs.Custom.Version = "0.23.0" // newer than deployed 0.22.1

	wb, err := h.BuildWorkflow(deployedNodeState(), emptyClusterState(), inputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wb == nil {
		t.Fatal("expected non-nil WorkflowBuilder")
	}
}

func TestUpgradeHandler_BuildWorkflow_WithStorageReset_Succeeds(t *testing.T) {
	h := newUpgradeHandler(nil)
	inputs := defaultUserInputs()
	inputs.Custom.Version = "0.23.0"
	inputs.Custom.ResetStorage = true

	wb, err := h.BuildWorkflow(deployedNodeState(), emptyClusterState(), inputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wb == nil {
		t.Fatal("expected non-nil WorkflowBuilder")
	}
}

// ── ResetHandler.BuildWorkflow ────────────────────────────────────────────────

func TestResetHandler_BuildWorkflow_NotDeployed_Errors(t *testing.T) {
	h := newResetHandler(nil)
	_, err := h.BuildWorkflow(notDeployedNodeState(), emptyClusterState(), defaultUserInputs())
	if err == nil {
		t.Fatal("expected error when block node is not deployed")
	}
}

func TestResetHandler_BuildWorkflow_Deployed_Succeeds(t *testing.T) {
	h := newResetHandler(nil)
	wb, err := h.BuildWorkflow(deployedNodeState(), emptyClusterState(), defaultUserInputs())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wb == nil {
		t.Fatal("expected non-nil WorkflowBuilder")
	}
}

func TestResetHandler_PrepareEffectiveInputs_PassesThrough(t *testing.T) {
	h := newResetHandler(nil)
	in := defaultUserInputs()
	out, err := h.PrepareEffectiveInputs(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != in {
		t.Error("expected reset handler to return the same inputs pointer unchanged")
	}
}

// ── UninstallHandler.BuildWorkflow ────────────────────────────────────────────

func TestUninstallHandler_BuildWorkflow_NotDeployed_WithoutForce_Errors(t *testing.T) {
	h := newUninstallHandler(nil)
	_, err := h.BuildWorkflow(notDeployedNodeState(), emptyClusterState(), defaultUserInputs())
	if err == nil {
		t.Fatal("expected error when block node is not deployed without --force")
	}
}

func TestUninstallHandler_BuildWorkflow_NotDeployed_WithForce_Succeeds(t *testing.T) {
	h := newUninstallHandler(nil)
	inputs := defaultUserInputs()
	inputs.Common.Force = true

	wb, err := h.BuildWorkflow(notDeployedNodeState(), emptyClusterState(), inputs)
	if err != nil {
		t.Fatalf("unexpected error with --force: %v", err)
	}
	if wb == nil {
		t.Fatal("expected non-nil WorkflowBuilder")
	}
}

func TestUninstallHandler_BuildWorkflow_Deployed_Succeeds(t *testing.T) {
	h := newUninstallHandler(nil)
	wb, err := h.BuildWorkflow(deployedNodeState(), emptyClusterState(), defaultUserInputs())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wb == nil {
		t.Fatal("expected non-nil WorkflowBuilder")
	}
}

func TestUninstallHandler_BuildWorkflow_WithStorageReset_Succeeds(t *testing.T) {
	h := newUninstallHandler(nil)
	inputs := defaultUserInputs()
	inputs.Custom.ResetStorage = true

	wb, err := h.BuildWorkflow(deployedNodeState(), emptyClusterState(), inputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wb == nil {
		t.Fatal("expected non-nil WorkflowBuilder")
	}
}

func TestUninstallHandler_PrepareEffectiveInputs_PassesThrough(t *testing.T) {
	h := newUninstallHandler(nil)
	in := defaultUserInputs()
	out, err := h.PrepareEffectiveInputs(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != in {
		t.Error("expected uninstall handler to return the same inputs pointer unchanged")
	}
}

// ── nil-input guards ──────────────────────────────────────────────────────────

func TestInstallHandler_PrepareEffectiveInputs_NilInputs_Errors(t *testing.T) {
	h := newInstallHandler(nil, nil)
	_, err := h.PrepareEffectiveInputs(nil)
	if err == nil {
		t.Fatal("expected error for nil inputs")
	}
}

func TestUpgradeHandler_PrepareEffectiveInputs_NilInputs_Errors(t *testing.T) {
	h := newUpgradeHandler(nil)
	_, err := h.PrepareEffectiveInputs(nil)
	if err == nil {
		t.Fatal("expected error for nil inputs")
	}
}

func TestResetHandler_PrepareEffectiveInputs_NilInputs_Errors(t *testing.T) {
	h := newResetHandler(nil)
	_, err := h.PrepareEffectiveInputs(nil)
	if err == nil {
		t.Fatal("expected error for nil inputs")
	}
}

func TestUninstallHandler_PrepareEffectiveInputs_NilInputs_Errors(t *testing.T) {
	h := newUninstallHandler(nil)
	_, err := h.PrepareEffectiveInputs(nil)
	if err == nil {
		t.Fatal("expected error for nil inputs")
	}
}
