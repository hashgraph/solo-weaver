// SPDX-License-Identifier: Apache-2.0

package bll

import (
	"os"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// ---- stub state manager --------------------------------------------------------

// stubStateManager is a hand-written test double for state.DefaultStateManager.
// It records every AddActionHistory call so tests can assert on the captured values
// without touching the filesystem or the hedera service account.
type stubStateManager struct {
	capturedActions []state.ActionHistory
	currentState    state.State
}

// compile-time check: stubStateManager must satisfy state.DefaultStateManager.
var _ state.DefaultStateManager = (*stubStateManager)(nil)

func newStubStateManager() *stubStateManager {
	return &stubStateManager{currentState: state.NewState()}
}

func (s *stubStateManager) State() state.State { return s.currentState }
func (s *stubStateManager) Set(st state.State) state.DefaultStateManager {
	s.currentState = st
	return s
}
func (s *stubStateManager) FileManager() fsx.Manager                      { return nil }
func (s *stubStateManager) Flush() error                                  { return nil }
func (s *stubStateManager) Refresh() error                                { return nil }
func (s *stubStateManager) HasPersistedState() (os.FileInfo, bool, error) { return nil, false, nil }
func (s *stubStateManager) AddActionHistory(entry state.ActionHistory) state.DefaultStateManager {
	s.capturedActions = append(s.capturedActions, entry)
	s.currentState.LastAction = entry
	return s
}

// ---- helpers -------------------------------------------------------------------

func successReport() *automa.Report {
	return &automa.Report{Status: automa.StatusSuccess}
}

func failedReport() *automa.Report {
	return &automa.Report{Status: automa.StatusFailed}
}

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

func newHandlerWithStub(sm *stubStateManager) BlockNodeIntentHandler {
	// Use a non-nil Registry with nil sub-fields; the tests that reach rsl calls
	// (StopOnError path) will fail at rsl.Cluster.RefreshState — that is intentional
	// and what the tests assert.
	return BlockNodeIntentHandler{sm: sm, rsl: &rsl.Registry{}}
}

// ---- tests ---------------------------------------------------------------------

// TestFlushState_NilReport verifies that a nil report returns an error immediately
// and that AddActionHistory is never called.
func TestFlushState_NilReport(t *testing.T) {
	sm := newStubStateManager()
	h := newHandlerWithStub(sm)

	_, err := h.flushState(nil, models.Intent{}, defaultInputs())
	if err == nil {
		t.Fatal("expected error for nil report, got nil")
	}

	if len(sm.capturedActions) != 0 {
		t.Errorf("expected no AddActionHistory calls for nil report, got %d", len(sm.capturedActions))
	}
}

// TestFlushState_FailedReport_SkipsWhenNotStopOnError verifies that a failed report with
// ContinueOnError execution mode causes flushState to skip state persistence entirely,
// returning the original report unchanged without calling AddActionHistory.
func TestFlushState_FailedReport_SkipsWhenNotStopOnError(t *testing.T) {
	sm := newStubStateManager()
	h := newHandlerWithStub(sm)

	inputs := defaultInputs()
	inputs.Common.ExecutionOptions.ExecutionMode = automa.ContinueOnError

	intent := models.Intent{Action: models.ActionInstall, Target: models.TargetBlocknode}
	returned, err := h.flushState(failedReport(), intent, inputs)
	if err != nil {
		t.Fatalf("expected no error when skipping, got: %v", err)
	}
	if returned == nil || !returned.IsFailed() {
		t.Error("expected the original failed report to be returned unchanged")
	}

	// AddActionHistory must NOT have been called — no state should be recorded for a skipped flush.
	if len(sm.capturedActions) != 0 {
		t.Errorf("expected no AddActionHistory calls when skip path taken, got %d", len(sm.capturedActions))
	}
}

// TestFlushState_FailedReport_ProceedsWhenStopOnError verifies that a failed report with
// StopOnError mode does NOT trigger the skip guard — it proceeds and calls AddActionHistory.
// The rsl singletons are nil so the function returns an error from the refresh call,
// but AddActionHistory must already have been recorded before that point.
func TestFlushState_FailedReport_ProceedsWhenStopOnError(t *testing.T) {
	sm := newStubStateManager()
	h := newHandlerWithStub(sm)

	inputs := defaultInputs() // ExecutionMode == StopOnError
	intent := models.Intent{Action: models.ActionInstall, Target: models.TargetBlocknode}

	_, err := h.flushState(failedReport(), intent, inputs)

	// rsl singleton is nil — an error from the refresh call is expected.
	if err == nil {
		t.Fatal("expected an error from nil rsl singleton, got nil")
	}

	// AddActionHistory must have been called before the rsl error.
	if len(sm.capturedActions) != 1 {
		t.Fatalf("expected 1 AddActionHistory call before rsl error, got %d", len(sm.capturedActions))
	}
}

// TestFlushState_SuccessReport_RecordsCorrectActionHistory verifies that a successful
// report causes AddActionHistory to be called with the exact intent and typed
// *models.UserInputs[models.BlocknodeInputs] — no manual map conversion.
func TestFlushState_SuccessReport_RecordsCorrectActionHistory(t *testing.T) {
	sm := newStubStateManager()
	h := newHandlerWithStub(sm)

	inputs := defaultInputs()
	intent := models.Intent{Action: models.ActionInstall, Target: models.TargetBlocknode}

	// rsl singletons are nil so an error is expected after AddActionHistory runs.
	_, _ = h.flushState(successReport(), intent, inputs)

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

	// Inputs must be the exact typed pointer — not a hand-rolled map.
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

// TestFlushState_SuccessReport_UpdatesLastActionInState verifies that AddActionHistory
// updates state.LastAction so it is included in the next Flush to state.yaml.
func TestFlushState_SuccessReport_UpdatesLastActionInState(t *testing.T) {
	sm := newStubStateManager()
	h := newHandlerWithStub(sm)

	intent := models.Intent{Action: models.ActionUpgrade, Target: models.TargetBlocknode}
	_, _ = h.flushState(successReport(), intent, defaultInputs())

	if sm.currentState.LastAction.Intent.Action != models.ActionUpgrade {
		t.Errorf("LastAction.Intent.Action: got %q, want %q",
			sm.currentState.LastAction.Intent.Action, models.ActionUpgrade)
	}
}
