// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package bll

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/golang/mock/gomock"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/ui"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/security/principal"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
)

type funcChecker[T any] func() (T, error)

func (f funcChecker[T]) RefreshState(context.Context) (T, error) { return f() }

// host stands in for the live system: the checkers read teleport from it, and
// the workflow can change it as a real install would. A non-nil teleportProbe
// swaps in the real Teleport checker with that cluster probe and no Helm releases.
type host struct {
	teleport      state.TeleportState
	teleportProbe reality.ClusterProbe
}

type noReleases struct{}

func (noReleases) ListAll() ([]*release.Release, error) { return nil, nil }

// noopIntent runs a one-step workflow that calls execute, or fails to build
// the workflow when buildErr is set.
type noopIntent struct {
	execute  func()
	buildErr error
}

func (n noopIntent) PrepareEffectiveInputs(_ models.Intent, in models.UserInputs[struct{}]) (*models.UserInputs[struct{}], error) {
	return &in, nil
}

func (n noopIntent) BuildWorkflow(state.State, models.UserInputs[struct{}]) (*automa.WorkflowBuilder, error) {
	if n.buildErr != nil {
		return nil, n.buildErr
	}
	step := automa.NewStepBuilder().WithId("noop").
		WithExecute(func(_ context.Context, stp automa.Step) *automa.Report {
			if n.execute != nil {
				n.execute()
			}
			return automa.SuccessReport(stp)
		})
	return automa.NewWorkflowBuilder().WithId("test").Steps(step), nil
}

func (n noopIntent) HandleIntent(context.Context, models.Intent, models.UserInputs[struct{}]) (*automa.Report, error) {
	return nil, nil
}

func newFileManager(t *testing.T) fsx.Manager {
	t.Helper()
	fm, err := fsx.NewManager(fsx.WithPrincipalManager(principal.NewMockManager(gomock.NewController(t))))
	require.NoError(t, err)
	return fm
}

func writeState(t *testing.T, stateFile string, teleport state.TeleportState) {
	t.Helper()
	s := state.NewState(stateFile)
	s.TeleportState = teleport
	sm, err := state.NewStateManager(state.WithState(s), state.WithFileManager(newFileManager(t)))
	require.NoError(t, err)
	require.NoError(t, sm.FlushState())
}

func readState(t *testing.T, stateFile string) state.State {
	t.Helper()
	sm, err := state.NewStateManager(state.WithState(state.NewState(stateFile)), state.WithFileManager(newFileManager(t)))
	require.NoError(t, err)
	require.NoError(t, sm.Refresh())
	return sm.State()
}

// runBlockNodeIntent runs one successful block node install through BaseHandler.
func runBlockNodeIntent(t *testing.T, stateFile string, h *host, intent noopIntent) *automa.Report {
	t.Helper()
	report, err := handleBlockNodeIntent(t, stateFile, h, intent)
	require.NoError(t, err)
	require.NoError(t, report.Error)
	return report
}

// handleBlockNodeIntent runs one block node install through BaseHandler the way
// a CLI command does: Setup's refresh, then HandleIntent, then the flush.
func handleBlockNodeIntent(t *testing.T, stateFile string, h *host, intent noopIntent) (*automa.Report, error) {
	t.Helper()
	return handleBlockNodeIntentWith(t, stateFile, h, intent, Teleport)
}

// handleBlockNodeIntentWith is handleBlockNodeIntent with the handler's managed components chosen.
func handleBlockNodeIntentWith(t *testing.T, stateFile string, h *host, intent noopIntent, managed ...Component) (*automa.Report, error) {
	t.Helper()
	sm, err := state.NewStateManager(state.WithState(state.NewState(stateFile)), state.WithFileManager(newFileManager(t)))
	require.NoError(t, err)
	require.NoError(t, sm.Refresh())

	checkers := reality.Checkers{
		Cluster: funcChecker[state.ClusterState](func() (state.ClusterState, error) {
			return state.ClusterState{Created: true}, nil
		}),
		Machine: funcChecker[state.MachineState](func() (state.MachineState, error) {
			return sm.State().MachineState, nil
		}),
		BlockNode: funcChecker[state.BlockNodeState](func() (state.BlockNodeState, error) {
			return sm.State().BlockNodeState, nil
		}),
		Consensus: funcChecker[map[string]state.ConsensusNodeState](func() (map[string]state.ConsensusNodeState, error) {
			return map[string]state.ConsensusNodeState{}, nil
		}),
		Teleport: funcChecker[state.TeleportState](func() (state.TeleportState, error) {
			return h.teleport, nil
		}),
	}
	if h.teleportProbe != nil {
		checkers.Teleport, err = reality.NewTeleportChecker(sm,
			func() (reality.HelmManager, error) { return noReleases{}, nil }, h.teleportProbe)
		require.NoError(t, err)
	}
	runtime, err := rsl.NewRuntimeResolver(models.Config{}, sm, checkers, time.Minute)
	require.NoError(t, err)
	base, err := NewBaseHandler[struct{}](runtime, models.TargetBlockNode, WithManagedComponents[struct{}](managed...))
	require.NoError(t, err)

	inputs := models.UserInputs[struct{}]{Common: models.CommonInputs{
		ExecutionOptions: models.WorkflowExecutionOptions{ExecutionMode: automa.StopOnError, RollbackMode: automa.StopOnError},
	}}
	return base.HandleIntent(context.Background(),
		models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode}, inputs, intent, nil)
}

func configuredNodeAgent() state.TeleportState {
	return state.TeleportState{NodeAgent: state.TeleportNodeAgentState{Installed: true, Configured: true}}
}

func TestHandleIntent_NoOutOfBandChangeHasNoWarning(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeState(t, stateFile, configuredNodeAgent())

	report := runBlockNodeIntent(t, stateFile, &host{teleport: configuredNodeAgent()}, noopIntent{})

	require.Empty(t, ui.CollectWarnings(report))
}

// A block node run reports a Teleport change, then flushes the live value as it
// always has, so the next run has nothing to report.
func TestHandleIntent_ReportsTeleportChangeThenPersistsLiveState(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeState(t, stateFile, configuredNodeAgent())
	h := &host{teleport: configuredNodeAgent()}
	h.teleport.NodeAgent.Configured = false

	report := runBlockNodeIntent(t, stateFile, h, noopIntent{})

	require.Equal(t, []string{
		`teleport nodeAgent.configured differs from persisted state: state.yaml has "true", live is "false"`,
	}, ui.CollectWarnings(report))
	require.Equal(t, h.teleport.NodeAgent, readState(t, stateFile).TeleportState.NodeAgent)

	second := runBlockNodeIntent(t, stateFile, h, noopIntent{})
	require.Empty(t, ui.CollectWarnings(second))
}

// A handler reports only the components it manages: with Teleport unmanaged, a
// Teleport change is neither reported nor absorbed — the persisted Teleport
// section survives the flush for its owning command to report later.
func TestHandleIntent_UnmanagedComponentIsNeitherReportedNorAbsorbed(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeState(t, stateFile, configuredNodeAgent())
	h := &host{teleport: configuredNodeAgent()}
	h.teleport.NodeAgent.Configured = false

	// Manage BlockNode only, so the live Teleport change is out of scope.
	report, err := handleBlockNodeIntentWith(t, stateFile, h, noopIntent{}, BlockNode)

	require.NoError(t, err)
	require.Empty(t, ui.CollectWarnings(report))
	// The out-of-band Teleport change is not folded into state: the persisted
	// baseline (Configured: true) is preserved, not the live value (false).
	require.Equal(t, configuredNodeAgent().NodeAgent, readState(t, stateFile).TeleportState.NodeAgent)
}

func TestHandleIntent_WorkflowsOwnChangeIsNotReported(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeState(t, stateFile, state.TeleportState{})
	h := &host{}

	report := runBlockNodeIntent(t, stateFile, h, noopIntent{execute: func() { h.teleport = configuredNodeAgent() }})

	require.Empty(t, ui.CollectWarnings(report))
	require.Equal(t, configuredNodeAgent().NodeAgent, readState(t, stateFile).TeleportState.NodeAgent)
}

// With no state file there is no baseline, so a component already on the host
// is not an out-of-band change.
func TestHandleIntent_NoStateFileHasNoWarning(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")

	report := runBlockNodeIntent(t, stateFile, &host{teleport: configuredNodeAgent()}, noopIntent{})

	require.Empty(t, ui.CollectWarnings(report))
	require.Equal(t, configuredNodeAgent().NodeAgent, readState(t, stateFile).TeleportState.NodeAgent)
}

func installedClusterAgent() state.TeleportState {
	return state.TeleportState{ClusterAgent: state.TeleportClusterAgentState{
		Installed: true, Release: "teleport-agent", Namespace: "teleport-agent", ChartVersion: "18.6.4",
	}}
}

// The cluster runtime reaches the cluster, then the Teleport checker's own probe
// finds the API server silent. The persisted cluster agent stands and nothing is
// reported.
func TestHandleIntent_UnobservableClusterIsNotReportedAsRemoval(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeState(t, stateFile, installedClusterAgent())
	h := &host{teleportProbe: func() reality.ClusterReachability { return reality.Unobservable }}

	report := runBlockNodeIntent(t, stateFile, h, noopIntent{})

	require.Empty(t, ui.CollectWarnings(report))
	require.Equal(t, installedClusterAgent().ClusterAgent, readState(t, stateFile).TeleportState.ClusterAgent)
}

// No kubeconfig is not proof the release is gone: nothing is reported.
func TestHandleIntent_NoKubeconfigIsNotReportedAsRemoval(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeState(t, stateFile, installedClusterAgent())
	h := &host{teleportProbe: func() reality.ClusterReachability { return reality.NotConfigured }}

	report := runBlockNodeIntent(t, stateFile, h, noopIntent{})

	require.Empty(t, ui.CollectWarnings(report))
	require.Equal(t, installedClusterAgent().ClusterAgent, readState(t, stateFile).TeleportState.ClusterAgent)
}

// A reachable cluster whose Helm releases lack the agent is a confirmed removal.
func TestHandleIntent_ReleaseMissingFromReachableClusterIsReportedAsRemoval(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeState(t, stateFile, installedClusterAgent())
	h := &host{teleportProbe: func() reality.ClusterReachability { return reality.Reachable }}

	report := runBlockNodeIntent(t, stateFile, h, noopIntent{})

	require.Equal(t, []string{
		`teleport clusterAgent.installed differs from persisted state: state.yaml has "true", live is "false"`,
	}, ui.CollectWarnings(report))
	require.Equal(t, state.TeleportClusterAgentState{}, readState(t, stateFile).TeleportState.ClusterAgent)
}

// captureLogs swaps the global logger for a buffer and restores it. Global
// state, so callers must not run in parallel.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevLogger, prevLevel := *logx.As(), zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	logx.SetLogger(zerolog.New(&buf))
	t.Cleanup(func() {
		logx.SetLogger(prevLogger)
		zerolog.SetGlobalLevel(prevLevel)
	})
	return &buf
}

// A run that fails before its workflow has no report, but logs the change and
// flushes nothing, so the next successful run still reports it.
func TestHandleIntent_EarlyFailureLogsTheChangeAndKeepsTheBaseline(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeState(t, stateFile, configuredNodeAgent())
	h := &host{teleport: configuredNodeAgent()}
	h.teleport.NodeAgent.Configured = false
	logs := captureLogs(t)

	_, err := handleBlockNodeIntent(t, stateFile, h, noopIntent{buildErr: errors.New("precondition failed")})

	require.Error(t, err)
	require.Contains(t, logs.String(), `"field":"nodeAgent.configured","persisted":"true","live":"false","message":"Live value differs from persisted state"`)
	require.True(t, readState(t, stateFile).TeleportState.NodeAgent.Configured, "nothing was flushed")

	report := runBlockNodeIntent(t, stateFile, h, noopIntent{})
	require.Equal(t, []string{
		`teleport nodeAgent.configured differs from persisted state: state.yaml has "true", live is "false"`,
	}, ui.CollectWarnings(report))
}
