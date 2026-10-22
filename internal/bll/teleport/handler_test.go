// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package teleport

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/golang/mock/gomock"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/drift"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/security/principal"
	"github.com/stretchr/testify/require"
)

type funcChecker[T any] func() (T, error)

func (f funcChecker[T]) RefreshState(context.Context) (T, error) { return f() }

func newRegistry(t *testing.T) *HandlerRegistry {
	t.Helper()
	return newRegistryWith(t, state.TeleportState{}, state.TeleportState{})
}

// newRegistryWith builds the real handlers over a state directory that records
// persisted, and a host whose Teleport agents are live. The kube environment
// points at no kubeconfig, so a cluster step that ran anyway would skip rather
// than reach the developer's cluster.
func newRegistryWith(t *testing.T, persisted, live state.TeleportState) *HandlerRegistry {
	t.Helper()
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "no-kubeconfig"))
	fm, err := fsx.NewManager(fsx.WithPrincipalManager(principal.NewMockManager(gomock.NewController(t))))
	require.NoError(t, err)
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	recorded := state.NewState(stateFile)
	recorded.TeleportState = persisted
	writer, err := state.NewStateManager(state.WithState(recorded), state.WithFileManager(fm))
	require.NoError(t, err)
	require.NoError(t, writer.FlushState())
	sm, err := state.NewStateManager(state.WithState(state.NewState(stateFile)), state.WithFileManager(fm))
	require.NoError(t, err)
	require.NoError(t, sm.Refresh())
	checkers := reality.Checkers{
		Cluster:   funcChecker[state.ClusterState](func() (state.ClusterState, error) { return state.ClusterState{}, nil }),
		Machine:   funcChecker[state.MachineState](func() (state.MachineState, error) { return state.MachineState{}, nil }),
		BlockNode: funcChecker[state.BlockNodeState](func() (state.BlockNodeState, error) { return state.BlockNodeState{}, nil }),
		Consensus: funcChecker[map[string]state.ConsensusNodeState](func() (map[string]state.ConsensusNodeState, error) {
			return map[string]state.ConsensusNodeState{}, nil
		}),
		Teleport: funcChecker[state.TeleportState](func() (state.TeleportState, error) { return live, nil }),
	}
	runtime, err := rsl.NewRuntimeResolver(models.Config{}, sm, checkers, time.Minute)
	require.NoError(t, err)
	reg, err := NewHandlerFactory(sm, runtime)
	require.NoError(t, err)
	return reg
}

// A teleport command flushes only its own agent: the other agent is kept as it
// was on disk, so a hand change to it stays on record for its own command.
func TestHandlerFactory_EachTargetKeepsTheOtherAgentAtBaseline(t *testing.T) {
	reg := newRegistry(t)
	var live, baseline state.State
	live.TeleportState = state.TeleportState{
		NodeAgent:    state.TeleportNodeAgentState{Installed: true},
		ClusterAgent: state.TeleportClusterAgentState{Installed: true, ChartVersion: "18.7.1-hotfix"},
	}
	baseline.TeleportState = state.TeleportState{
		NodeAgent:    state.TeleportNodeAgentState{Installed: true, Configured: true},
		ClusterAgent: state.TeleportClusterAgentState{Installed: true, ChartVersion: "18.6.4"},
	}

	nodeChange := drift.Change{Component: "teleport", Field: "nodeAgent.configured"}
	clusterChange := drift.Change{Component: "teleport", Field: "clusterAgent.chartVersion"}

	for name, base := range map[string]bll.BaseHandler[models.TeleportNodeInputs]{
		"node install":   reg.nodeInstall.BaseHandler,
		"node uninstall": reg.nodeUninstall.BaseHandler,
	} {
		require.NotNil(t, base.KeptAtBaseline, name)
		got := live
		base.KeptAtBaseline(&got, baseline)
		require.Equal(t, baseline.TeleportState.ClusterAgent, got.TeleportState.ClusterAgent, name)
		require.Equal(t, live.TeleportState.NodeAgent, got.TeleportState.NodeAgent, name)
		require.True(t, base.KeptChange(clusterChange), name)
		require.False(t, base.KeptChange(nodeChange), name)
	}
	for name, base := range map[string]bll.BaseHandler[models.TeleportClusterInputs]{
		"cluster install":   reg.clusterInstall.BaseHandler,
		"cluster uninstall": reg.clusterUninstall.BaseHandler,
	} {
		require.NotNil(t, base.KeptAtBaseline, name)
		got := live
		base.KeptAtBaseline(&got, baseline)
		require.Equal(t, baseline.TeleportState.NodeAgent, got.TeleportState.NodeAgent, name)
		require.Equal(t, live.TeleportState.ClusterAgent, got.TeleportState.ClusterAgent, name)
		require.True(t, base.KeptChange(nodeChange), name)
		require.False(t, base.KeptChange(clusterChange), name)
	}
}

// The real cluster uninstall stops on a hand change to its agent, before its
// workflow runs.
func TestClusterUninstall_StopsOnOwnedDrift(t *testing.T) {
	intent := models.Intent{Action: models.ActionUninstall, Target: models.TargetTeleportCluster}
	common := models.CommonInputs{ExecutionOptions: models.WorkflowExecutionOptions{
		ExecutionMode: automa.StopOnError, RollbackMode: automa.ContinueOnError,
	}}

	_, err := newRegistryWith(t, clusterAgent(true, "18.6.4"), clusterAgent(true, "18.7.1-hotfix")).clusterUninstall.HandleIntent(
		context.Background(), intent, models.UserInputs[models.TeleportClusterInputs]{Common: common})

	require.ErrorContains(t, err, "would overwrite out-of-band changes to teleport")
	reason, ok := errx.ReasonOf(err)
	require.True(t, ok, "%v", err)
	require.Equal(t, reasons.PreconditionNotMet, reason)
}

// Both uninstalls keep the guard flags in their effective inputs, which is
// what the guard reads.
func TestUninstall_EffectiveInputsKeepTheGuardFlags(t *testing.T) {
	reg := newRegistry(t)
	common := models.CommonInputs{Force: true, OverrideDrift: true, NonInteractive: true,
		ExecutionOptions: models.WorkflowExecutionOptions{ExecutionMode: automa.StopOnError, RollbackMode: automa.ContinueOnError},
	}

	node, err := reg.nodeUninstall.PrepareEffectiveInputs(
		models.Intent{Action: models.ActionUninstall, Target: models.TargetTeleportNode},
		models.UserInputs[models.TeleportNodeInputs]{Common: common})
	require.NoError(t, err)
	require.Equal(t, common, node.Common)

	cluster, err := reg.clusterUninstall.PrepareEffectiveInputs(
		models.Intent{Action: models.ActionUninstall, Target: models.TargetTeleportCluster},
		models.UserInputs[models.TeleportClusterInputs]{Common: common})
	require.NoError(t, err)
	require.Equal(t, common, cluster.Common)
}
