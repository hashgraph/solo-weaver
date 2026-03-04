// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"testing"

	"helm.sh/helm/v3/pkg/release"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

func TestBuildWorkflow_AlreadyDeployedWithoutForce_ReturnsError(t *testing.T) {
	h := newInstallHandler(nil, nil)

	nodeState := state.BlockNodeState{}
	nodeState.ReleaseInfo.Status = release.StatusDeployed

	clusterState := state.ClusterState{Created: true}

	in := &models.UserInputs[models.BlocknodeInputs]{}
	in.Common.Force = false

	wb, err := h.BuildWorkflow(nodeState, clusterState, in)
	if err == nil {
		t.Fatalf("expected error when node already deployed and force is false, got nil and workflow: %+v", wb)
	}
}

func TestBuildWorkflow_AlreadyDeployedWithForce_ReturnsWorkflow(t *testing.T) {
	h := newInstallHandler(nil, nil)

	nodeState := state.BlockNodeState{}
	nodeState.ReleaseInfo.Status = release.StatusDeployed

	clusterState := state.ClusterState{Created: true}

	in := &models.UserInputs[models.BlocknodeInputs]{}
	in.Common.Force = true

	wb, err := h.BuildWorkflow(nodeState, clusterState, in)
	if err != nil {
		t.Fatalf("unexpected error when force is true: %v", err)
	}
	if wb == nil {
		t.Fatalf("expected a workflow builder when force is true, got nil")
	}
}

func TestBuildWorkflow_ClusterCreatedAndNotDeployed_ReturnsWorkflow(t *testing.T) {
	h := newInstallHandler(nil, nil)

	nodeState := state.BlockNodeState{}
	nodeState.ReleaseInfo.Status = "" // not deployed

	clusterState := state.ClusterState{Created: true}

	in := &models.UserInputs[models.BlocknodeInputs]{}
	in.Common.Force = false

	wb, err := h.BuildWorkflow(nodeState, clusterState, in)
	if err != nil {
		t.Fatalf("unexpected error for created cluster: %v", err)
	}
	if wb == nil {
		t.Fatalf("expected a workflow builder for created cluster, got nil")
	}
}
