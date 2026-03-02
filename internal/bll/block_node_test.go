// SPDX-License-Identifier: Apache-2.0

package bll

import (
	"os"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// ---- stub state reader ---------------------------------------------------------

// stubStateReader satisfies state.Reader for tests that only need read access.
type stubStateReader struct {
	currentState state.State
}

var _ state.Reader = (*stubStateReader)(nil)

func newStubStateReader() *stubStateReader {
	return &stubStateReader{currentState: state.NewState()}
}

func (r *stubStateReader) State() state.State                            { return r.currentState }
func (r *stubStateReader) HasPersistedState() (os.FileInfo, bool, error) { return nil, false, nil }

// ---- stub state writer ---------------------------------------------------------

// stubStateWriter satisfies state.Writer for tests.
// It records every AddActionHistory call so tests can assert on captured values.
type stubStateWriter struct {
	capturedActions []state.ActionHistory
	reader          *stubStateReader // shared so Set() is visible to reader.State()
}

var _ state.Writer = (*stubStateWriter)(nil)

func (w *stubStateWriter) Set(st state.State) state.DefaultStateManager {
	w.reader.currentState = st
	return nil // chaining not needed in tests
}
func (w *stubStateWriter) Flush() error { return nil }
func (w *stubStateWriter) AddActionHistory(entry state.ActionHistory) state.DefaultStateManager {
	w.capturedActions = append(w.capturedActions, entry)
	w.reader.currentState.LastAction = entry
	return nil // chaining not needed in tests
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

// newHandlerWithStubs builds a BlockNodeIntentHandler with the supplied reader/writer pair.
// A non-nil rsl.Registry with nil sub-fields is used so tests that reach rsl calls
// (StopOnError path) receive a controlled error rather than a nil-pointer panic.
func newHandlerWithStubs(reader *stubStateReader, writer *stubStateWriter) BlockNodeIntentHandler {
	return BlockNodeIntentHandler{
		stateReader: reader,
		stateWriter: writer,
		rsl:         &rsl.Registry{},
	}
}

// newHandlerWithStub is kept for convenience when a test doesn't need to inspect
// writer state separately.
func newHandlerWithStub(writer *stubStateWriter) BlockNodeIntentHandler {
	return newHandlerWithStubs(writer.reader, writer)
}

// newTestPair creates a linked reader/writer pair.
func newTestPair() (*stubStateReader, *stubStateWriter) {
	r := newStubStateReader()
	w := &stubStateWriter{reader: r}
	return r, w
}

// ---- tests ---------------------------------------------------------------------

// TestFlushState_NilReport verifies that a nil report returns an error immediately
// and that AddActionHistory is never called.
func TestFlushState_NilReport(t *testing.T) {
	_, w := newTestPair()
	h := newHandlerWithStub(w)

	_, err := h.flushState(nil, models.Intent{}, defaultInputs())
	if err == nil {
		t.Fatal("expected error for nil report, got nil")
	}

	if len(w.capturedActions) != 0 {
		t.Errorf("expected no AddActionHistory calls for nil report, got %d", len(w.capturedActions))
	}
}

// TestFlushState_FailedReport_SkipsWhenNotStopOnError verifies that a failed report with
// ContinueOnError execution mode causes flushState to skip state persistence entirely,
// returning the original report unchanged without calling AddActionHistory.
func TestFlushState_FailedReport_SkipsWhenNotStopOnError(t *testing.T) {
	_, w := newTestPair()
	h := newHandlerWithStub(w)

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

	if len(w.capturedActions) != 0 {
		t.Errorf("expected no AddActionHistory calls when skip path taken, got %d", len(w.capturedActions))
	}
}

// TestFlushState_FailedReport_ProceedsWhenStopOnError verifies that a failed report with
// StopOnError mode does NOT trigger the skip guard — it proceeds and calls AddActionHistory.
// The rsl singletons are nil so the function returns an error from the refresh call,
// but AddActionHistory must already have been recorded before that point.
func TestFlushState_FailedReport_ProceedsWhenStopOnError(t *testing.T) {
	_, w := newTestPair()
	h := newHandlerWithStub(w)

	inputs := defaultInputs() // ExecutionMode == StopOnError
	intent := models.Intent{Action: models.ActionInstall, Target: models.TargetBlocknode}

	_, err := h.flushState(failedReport(), intent, inputs)

	// rsl registry has nil sub-fields — an error from the refresh call is expected.
	if err == nil {
		t.Fatal("expected an error from nil rsl sub-field, got nil")
	}

	// AddActionHistory must have been called before the rsl error.
	if len(w.capturedActions) != 1 {
		t.Fatalf("expected 1 AddActionHistory call before rsl error, got %d", len(w.capturedActions))
	}
}

// TestFlushState_SuccessReport_RecordsCorrectActionHistory verifies that a successful
// report causes AddActionHistory to be called with the exact intent and typed
// *models.UserInputs[models.BlocknodeInputs] — no manual map conversion.
func TestFlushState_SuccessReport_RecordsCorrectActionHistory(t *testing.T) {
	_, w := newTestPair()
	h := newHandlerWithStub(w)

	inputs := defaultInputs()
	intent := models.Intent{Action: models.ActionInstall, Target: models.TargetBlocknode}

	// rsl sub-fields are nil so an error is expected after AddActionHistory runs.
	_, _ = h.flushState(successReport(), intent, inputs)

	if len(w.capturedActions) != 1 {
		t.Fatalf("expected 1 captured action, got %d", len(w.capturedActions))
	}

	captured := w.capturedActions[0]

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

// TestFlushState_SuccessReport_UpdatesLastActionInState verifies that AddActionHistory
// updates state.LastAction so it is included in the next Flush to state.yaml.
func TestFlushState_SuccessReport_UpdatesLastActionInState(t *testing.T) {
	r, w := newTestPair()
	h := newHandlerWithStubs(r, w)

	intent := models.Intent{Action: models.ActionUpgrade, Target: models.TargetBlocknode}
	_, _ = h.flushState(successReport(), intent, defaultInputs())

	if r.currentState.LastAction.Intent.Action != models.ActionUpgrade {
		t.Errorf("LastAction.Intent.Action: got %q, want %q",
			r.currentState.LastAction.Intent.Action, models.ActionUpgrade)
	}
}
