// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package teleport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
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
	"gopkg.in/yaml.v3"
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
	return newRegistryReading(t, persisted, func() state.TeleportState { return live })
}

// newRegistryReading is newRegistryWith with the Teleport agents read from
// live on every refresh.
func newRegistryReading(t *testing.T, persisted state.TeleportState, live func() state.TeleportState) *HandlerRegistry {
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
		Teleport: funcChecker[state.TeleportState](func() (state.TeleportState, error) { return live(), nil }),
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

// stubCluster is a local API server that answers every request with its
// version. It counts the requests it gets, and the deletes among them.
type stubCluster struct {
	requests, deletes atomic.Int32
}

// serveStubCluster points the kube environment at a new stubCluster.
func serveStubCluster(t *testing.T) *stubCluster {
	t.Helper()
	var stub stubCluster
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.requests.Add(1)
		if r.Method == http.MethodDelete {
			stub.deletes.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"major":"1","minor":"33","gitVersion":"v1.33.4"}`))
	}))
	t.Cleanup(srv.Close)
	kubeconfig := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfig, []byte(fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: stub
  cluster:
    server: %s
contexts:
- name: stub
  context:
    cluster: stub
    user: stub
current-context: stub
users:
- name: stub
  user: {}
`, srv.URL)), 0o600))
	t.Setenv("KUBECONFIG", kubeconfig)
	return &stub
}

// lastOwnedDrift reads the newest action history entry's owned drift.
func lastOwnedDrift(t *testing.T, reg *HandlerRegistry) []state.OwnedDriftRecord {
	t.Helper()
	b, err := os.ReadFile(state.ActionHistoryFilePath(reg.clusterUninstall.Runtime.StateDir()))
	require.NoError(t, err)
	var last state.ActionHistory
	dec := yaml.NewDecoder(bytes.NewReader(b))
	for {
		var e state.ActionHistory
		err := dec.Decode(&e)
		if errors.Is(err, io.EOF) {
			return last.OwnedDrift
		}
		require.NoError(t, err)
		last = e
	}
}

// uninstallWith runs the real cluster uninstall with common's guard flags.
func uninstallWith(reg *HandlerRegistry, common models.CommonInputs) (*automa.Report, error) {
	common.ExecutionOptions = models.WorkflowExecutionOptions{ExecutionMode: automa.StopOnError, RollbackMode: automa.ContinueOnError}
	return reg.clusterUninstall.HandleIntent(context.Background(),
		models.Intent{Action: models.ActionUninstall, Target: models.TargetTeleportCluster},
		models.UserInputs[models.TeleportClusterInputs]{Common: common})
}

// uninstallOverridden runs the real cluster uninstall with --override-drift.
func uninstallOverridden(t *testing.T, reg *HandlerRegistry) *automa.Report {
	t.Helper()
	report, err := uninstallWith(reg, models.CommonInputs{OverrideDrift: true})
	require.NoError(t, err)
	return report
}

// The race from review: the pre-run refresh could not read the cluster agent,
// and the cluster may answer by the time the step would run. No flag gets past
// that: --override-drift, --force and a non-interactive run all stop with
// "cannot verify", and the cluster is never asked anything.
func TestClusterUninstall_NoFlagGetsPastAnAgentTheRefreshCouldNotRead(t *testing.T) {
	for name, common := range map[string]models.CommonInputs{
		"override":        {OverrideDrift: true},
		"force":           {Force: true},
		"non-interactive": {NonInteractive: true},
	} {
		t.Run(name, func(t *testing.T) {
			unread := clusterAgent(true, "18.6.4")
			unread.ClusterAgent.Unobserved = true
			reg := newRegistryWith(t, clusterAgent(true, "18.6.4"), unread)
			cluster := serveStubCluster(t)

			_, err := uninstallWith(reg, common)

			require.ErrorContains(t, err, "teleport-cluster uninstall cannot verify teleport:")
			require.Zero(t, cluster.requests.Load(), "the run must not ask the cluster anything")
			require.NoFileExists(t, state.ActionHistoryFilePath(reg.clusterUninstall.Runtime.StateDir()))
		})
	}
}

// The same overridden run over an agent the refresh did read reaches the
// cluster, so the stub above is reachable. The stub has no release, so the
// step skips. The Teleport read follows the stub: the agent reads as gone once
// the stub has seen a delete, so the test fails if the step uninstalls. The
// entry says the run was allowed to destroy the change, and that it did not.
func TestClusterUninstall_SkippedUninstallIsAllowedButNotConfirmedDestroyed(t *testing.T) {
	var cluster *stubCluster
	reg := newRegistryReading(t, clusterAgent(true, "18.6.4"), func() state.TeleportState {
		if cluster != nil && cluster.deletes.Load() > 0 {
			return clusterAgent(false, "")
		}
		return clusterAgent(true, "18.7.1-hotfix")
	})
	cluster = serveStubCluster(t)

	report := uninstallOverridden(t, reg)

	require.NoError(t, report.Error)
	require.NotZero(t, cluster.requests.Load())
	require.Zero(t, cluster.deletes.Load())
	require.Equal(t, []state.OwnedDriftRecord{{
		Component: "teleport", Field: "clusterAgent.chartVersion", Persisted: "18.6.4", Live: "18.7.1-hotfix",
		DestroyAllowed: true,
	}}, lastOwnedDrift(t, reg))
}
