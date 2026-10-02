// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package bll

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/automa-saga/automa"
	"github.com/golang/mock/gomock"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/ui"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/security/principal"
	"github.com/stretchr/testify/require"
)

type funcChecker[T any] func() (T, error)

func (f funcChecker[T]) RefreshState(context.Context) (T, error) { return f() }

// host stands in for the live system: the checkers read teleport from it, and
// the workflow can change it as a real install would.
type host struct {
	teleport state.TeleportState
}

// noopIntent runs a one-step workflow that calls execute.
type noopIntent struct {
	execute func()
}

func (n noopIntent) PrepareEffectiveInputs(_ models.Intent, in models.UserInputs[struct{}]) (*models.UserInputs[struct{}], error) {
	return &in, nil
}

func (n noopIntent) BuildWorkflow(state.State, models.UserInputs[struct{}]) (*automa.WorkflowBuilder, error) {
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

// runBlockNodeIntent runs one block node install through BaseHandler the way a
// CLI command does: Setup's refresh, then HandleIntent, then the flush.
func runBlockNodeIntent(t *testing.T, stateFile string, h *host, intent noopIntent) *automa.Report {
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
	runtime, err := rsl.NewRuntimeResolver(models.Config{}, sm, checkers, time.Minute)
	require.NoError(t, err)
	base, err := NewBaseHandler[struct{}](runtime, models.TargetBlockNode)
	require.NoError(t, err)

	inputs := models.UserInputs[struct{}]{Common: models.CommonInputs{
		ExecutionOptions: models.WorkflowExecutionOptions{ExecutionMode: automa.StopOnError, RollbackMode: automa.StopOnError},
	}}
	report, err := base.HandleIntent(context.Background(),
		models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode}, inputs, intent, nil)
	require.NoError(t, err)
	require.NoError(t, report.Error)
	return report
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
		`teleport nodeAgent.configured changed outside solo-provisioner: state.yaml has "true", live is "false"`,
	}, ui.CollectWarnings(report))
	require.Equal(t, h.teleport.NodeAgent, readState(t, stateFile).TeleportState.NodeAgent)

	second := runBlockNodeIntent(t, stateFile, h, noopIntent{})
	require.Empty(t, ui.CollectWarnings(second))
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
