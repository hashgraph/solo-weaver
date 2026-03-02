// SPDX-License-Identifier: Apache-2.0

package bll

// base_handler_test.go tests the shared FlushNodeState infrastructure.
//
// stubStateManager satisfies state.StateManager without any I/O, giving tests
// full read+write access through a single stub — matching the production design
// where one StateManager instance is passed throughout the bll layer.

import (
	"os"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// ── stub state manager ────────────────────────────────────────────────────────

type stubStateManager struct {
	currentState    state.State
	capturedActions []state.ActionHistory
}

var _ state.Manager = (*stubStateManager)(nil)

func newStubStateManager() *stubStateManager {
	return &stubStateManager{currentState: state.NewState()}
}

// state.Reader
func (s *stubStateManager) State() state.State                            { return s.currentState }
func (s *stubStateManager) HasPersistedState() (os.FileInfo, bool, error) { return nil, false, nil }

// state.Writer
func (s *stubStateManager) Set(st state.State) state.Writer {
	s.currentState = st
	return s
}
func (s *stubStateManager) Flush() error { return nil }
func (s *stubStateManager) AddActionHistory(entry state.ActionHistory) state.Writer {
	s.capturedActions = append(s.capturedActions, entry)
	s.currentState.LastAction = entry
	return s
}

// state.Persister
func (s *stubStateManager) Refresh() error           { return nil }
func (s *stubStateManager) FileManager() fsx.Manager { return nil }

// ── helpers ───────────────────────────────────────────────────────────────────

func newStubSM() *stubStateManager { return newStubStateManager() }

func newBaseWithStubs(sm *stubStateManager) NodeHandlerBase {
	return NodeHandlerBase{
		StateManager: sm,
		RSL:          &rsl.Registry{}, // nil sub-fields trigger a controlled error on refresh
	}
}

func successReport() *automa.Report { return &automa.Report{Status: automa.StatusSuccess} }
func failedReport() *automa.Report  { return &automa.Report{Status: automa.StatusFailed} }

func defaultInputs() *models.UserInputs[models.BlocknodeInputs] {
	return &models.UserInputs[models.BlocknodeInputs]{
		Common: models.CommonInputs{
			ExecutionOptions: models.WorkflowExecutionOptions{
				ExecutionMode: automa.StopOnError,
				RollbackMode:  automa.StopOnError,
			},
		},
		Custom: models.BlocknodeInputs{
			Profile:   "local",
			Namespace: "block-node-ns",
			Release:   "block-node",
			Version:   "0.22.1",
			Chart:     "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server",
		},
	}
}

// ── FlushNodeState tests ──────────────────────────────────────────────────────

// TestFlushNodeState_NilReport verifies that a nil report returns an error
// immediately and that AddActionHistory is never called.
func TestFlushNodeState_NilReport(t *testing.T) {
	sm := newStubSM()
	base := newBaseWithStubs(sm)

	_, err := FlushNodeState(base, nil, models.Intent{}, defaultInputs(), nil)
	if err == nil {
		t.Fatal("expected error for nil report, got nil")
	}
	if len(sm.capturedActions) != 0 {
		t.Errorf("expected no AddActionHistory calls for nil report, got %d", len(sm.capturedActions))
	}
}

// TestFlushNodeState_FailedReport_SkipsWhenContinueOnError verifies that a
// failed report with ContinueOnError skips state persistence entirely.
func TestFlushNodeState_FailedReport_SkipsWhenContinueOnError(t *testing.T) {
	sm := newStubSM()
	base := newBaseWithStubs(sm)

	inputs := defaultInputs()
	inputs.Common.ExecutionOptions.ExecutionMode = automa.ContinueOnError

	intent := models.Intent{Action: models.ActionInstall, Target: models.TargetBlocknode}
	returned, err := FlushNodeState(base, failedReport(), intent, inputs, nil)
	if err != nil {
		t.Fatalf("expected no error on skip path, got: %v", err)
	}
	if returned == nil || !returned.IsFailed() {
		t.Error("expected the original failed report to be returned unchanged")
	}
	if len(sm.capturedActions) != 0 {
		t.Errorf("expected no AddActionHistory calls on skip path, got %d", len(sm.capturedActions))
	}
}

// TestFlushNodeState_FailedReport_ProceedsWhenStopOnError verifies that a
// failed report with StopOnError does NOT trigger the ContinueOnError skip
// guard: rollback will have run, and the post-rollback state should still be
// persisted. AddActionHistory is called before the rsl refresh error.
func TestFlushNodeState_FailedReport_ProceedsWhenStopOnError(t *testing.T) {
	sm := newStubSM()
	base := newBaseWithStubs(sm)

	inputs := defaultInputs() // ExecutionMode == StopOnError
	intent := models.Intent{Action: models.ActionInstall, Target: models.TargetBlocknode}

	_, err := FlushNodeState(base, failedReport(), intent, inputs, nil)

	// The rsl.Registry has nil sub-fields — a refresh error is expected.
	if err == nil {
		t.Fatal("expected error from nil rsl sub-field, got nil")
	}
	// AddActionHistory must have been called before the rsl error.
	if len(sm.capturedActions) != 1 {
		t.Fatalf("expected 1 AddActionHistory call before rsl error, got %d", len(sm.capturedActions))
	}
}

// TestFlushNodeState_SuccessReport_RecordsCorrectActionHistory verifies that a
// successful report causes AddActionHistory to record the exact intent and
// typed inputs pointer.
func TestFlushNodeState_SuccessReport_RecordsCorrectActionHistory(t *testing.T) {
	sm := newStubSM()
	base := newBaseWithStubs(sm)

	inputs := defaultInputs()
	intent := models.Intent{Action: models.ActionInstall, Target: models.TargetBlocknode}

	// rsl sub-fields are nil so a refresh error is expected after history is recorded.
	_, _ = FlushNodeState(base, successReport(), intent, inputs, nil)

	if len(sm.capturedActions) != 1 {
		t.Fatalf("expected 1 captured action, got %d", len(sm.capturedActions))
	}
	captured := sm.capturedActions[0]

	if captured.Intent.Action != models.ActionInstall {
		t.Errorf("Intent.Action: got %q, want %q", captured.Intent.Action, models.ActionInstall)
	}
	if captured.Intent.Target != models.TargetBlocknode {
		t.Errorf("Intent.Target: got %q, want %q", captured.Intent.Target, models.TargetBlocknode)
	}

	capturedInputs, ok := captured.Inputs.(*models.UserInputs[models.BlocknodeInputs])
	if !ok {
		t.Fatalf("Inputs type: got %T, want *models.UserInputs[models.BlocknodeInputs]", captured.Inputs)
	}
	if capturedInputs.Custom.Namespace != "block-node-ns" {
		t.Errorf("Custom.Namespace: got %q, want %q", capturedInputs.Custom.Namespace, "block-node-ns")
	}
	if capturedInputs.Custom.Version != "0.22.1" {
		t.Errorf("Custom.Version: got %q, want %q", capturedInputs.Custom.Version, "0.22.1")
	}
	if capturedInputs.Custom.Chart != "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server" {
		t.Errorf("Custom.Chart: got %q, want %q", capturedInputs.Custom.Chart,
			"oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server")
	}
}

// TestFlushNodeState_SuccessReport_UpdatesLastActionInState verifies that
// AddActionHistory updates state.LastAction so it is included in the next
// Flush to state.yaml.
func TestFlushNodeState_SuccessReport_UpdatesLastActionInState(t *testing.T) {
	sm := newStubSM()
	base := newBaseWithStubs(sm)

	intent := models.Intent{Action: models.ActionUpgrade, Target: models.TargetBlocknode}
	_, _ = FlushNodeState(base, successReport(), intent, defaultInputs(), nil)

	if sm.currentState.LastAction.Intent.Action != models.ActionUpgrade {
		t.Errorf("LastAction.Intent.Action: got %q, want %q",
			sm.currentState.LastAction.Intent.Action, models.ActionUpgrade)
	}
}

// TestNewNodeHandlerBase_NilDependencies verifies that nil dependencies are
// rejected at construction time rather than causing a nil-pointer panic later.
func TestNewNodeHandlerBase_NilStateManager(t *testing.T) {
	_, err := NewNodeHandlerBase(nil, &rsl.Registry{})
	if err == nil {
		t.Fatal("expected error for nil state.Manager")
	}
}

func TestNewNodeHandlerBase_NilRegistry(t *testing.T) {
	sm := newStubSM()
	_, err := NewNodeHandlerBase(sm, nil)
	if err == nil {
		t.Fatal("expected error for nil registry")
	}
}

func TestNewNodeHandlerBase_AllValid(t *testing.T) {
	sm := newStubSM()
	base, err := NewNodeHandlerBase(sm, &rsl.Registry{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if base.StateManager == nil || base.RSL == nil {
		t.Fatal("expected all fields to be set")
	}
}
