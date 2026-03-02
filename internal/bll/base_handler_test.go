// SPDX-License-Identifier: Apache-2.0

package bll

// base_handler_test.go tests the shared FlushNodeState infrastructure.
//
// The stubs below satisfy state.Reader and state.Writer without any I/O.
// A *rsl.Registry with nil sub-fields is injected for the paths that reach
// the rsl refresh calls — those return a controlled error rather than a
// nil-pointer panic, which is sufficient to verify ordering guarantees
// (AddActionHistory is called before the rsl refresh).

import (
	"os"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// ── stub state reader ─────────────────────────────────────────────────────────

type stubStateReader struct {
	currentState state.State
}

var _ state.Reader = (*stubStateReader)(nil)

func newStubStateReader() *stubStateReader {
	return &stubStateReader{currentState: state.NewState()}
}

func (r *stubStateReader) State() state.State                            { return r.currentState }
func (r *stubStateReader) HasPersistedState() (os.FileInfo, bool, error) { return nil, false, nil }

// ── stub state writer ─────────────────────────────────────────────────────────

type stubStateWriter struct {
	capturedActions []state.ActionHistory
	reader          *stubStateReader // shared so Set() updates are visible via reader.State()
}

var _ state.Writer = (*stubStateWriter)(nil)

func (w *stubStateWriter) Set(st state.State) state.Writer {
	w.reader.currentState = st
	return w
}
func (w *stubStateWriter) Flush() error { return nil }
func (w *stubStateWriter) AddActionHistory(entry state.ActionHistory) state.Writer {
	w.capturedActions = append(w.capturedActions, entry)
	w.reader.currentState.LastAction = entry
	return w
}

// ── helpers ───────────────────────────────────────────────────────────────────

func newTestPair() (*stubStateReader, *stubStateWriter) {
	r := newStubStateReader()
	return r, &stubStateWriter{reader: r}
}

func newBaseWithStubs(r *stubStateReader, w *stubStateWriter) NodeHandlerBase {
	return NodeHandlerBase{
		StateReader: r,
		StateWriter: w,
		RSL:         &rsl.Registry{}, // nil sub-fields trigger a controlled error on refresh
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
	_, w := newTestPair()
	base := newBaseWithStubs(w.reader, w)

	_, err := FlushNodeState(base, nil, models.Intent{}, defaultInputs(), nil)
	if err == nil {
		t.Fatal("expected error for nil report, got nil")
	}
	if len(w.capturedActions) != 0 {
		t.Errorf("expected no AddActionHistory calls for nil report, got %d", len(w.capturedActions))
	}
}

// TestFlushNodeState_FailedReport_SkipsWhenContinueOnError verifies that a
// failed report with ContinueOnError skips state persistence entirely because
// the resulting cluster state is indeterminate — some steps may have been
// partially applied or skipped.
func TestFlushNodeState_FailedReport_SkipsWhenContinueOnError(t *testing.T) {
	_, w := newTestPair()
	base := newBaseWithStubs(w.reader, w)

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
	if len(w.capturedActions) != 0 {
		t.Errorf("expected no AddActionHistory calls on skip path, got %d", len(w.capturedActions))
	}
}

// TestFlushNodeState_FailedReport_ProceedsWhenStopOnError verifies that a
// failed report with StopOnError does NOT trigger the ContinueOnError skip
// guard: rollback will have run, and the post-rollback state should still be
// persisted. AddActionHistory is called before the rsl refresh error.
func TestFlushNodeState_FailedReport_ProceedsWhenStopOnError(t *testing.T) {
	_, w := newTestPair()
	base := newBaseWithStubs(w.reader, w)

	inputs := defaultInputs() // ExecutionMode == StopOnError
	intent := models.Intent{Action: models.ActionInstall, Target: models.TargetBlocknode}

	_, err := FlushNodeState(base, failedReport(), intent, inputs, nil)

	// The rsl.Registry has nil sub-fields — a refresh error is expected.
	if err == nil {
		t.Fatal("expected error from nil rsl sub-field, got nil")
	}
	// AddActionHistory must have been called before the rsl error.
	if len(w.capturedActions) != 1 {
		t.Fatalf("expected 1 AddActionHistory call before rsl error, got %d", len(w.capturedActions))
	}
}

// TestFlushNodeState_SuccessReport_RecordsCorrectActionHistory verifies that a
// successful report causes AddActionHistory to record the exact intent and
// typed inputs pointer — no lossy map conversion.
func TestFlushNodeState_SuccessReport_RecordsCorrectActionHistory(t *testing.T) {
	_, w := newTestPair()
	base := newBaseWithStubs(w.reader, w)

	inputs := defaultInputs()
	intent := models.Intent{Action: models.ActionInstall, Target: models.TargetBlocknode}

	// rsl sub-fields are nil so a refresh error is expected after history is recorded.
	_, _ = FlushNodeState(base, successReport(), intent, inputs, nil)

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

// TestFlushNodeState_SuccessReport_UpdatesLastActionInState verifies that
// AddActionHistory updates state.LastAction so it is included in the next
// Flush to state.yaml.
func TestFlushNodeState_SuccessReport_UpdatesLastActionInState(t *testing.T) {
	r, w := newTestPair()
	base := newBaseWithStubs(r, w)

	intent := models.Intent{Action: models.ActionUpgrade, Target: models.TargetBlocknode}
	_, _ = FlushNodeState(base, successReport(), intent, defaultInputs(), nil)

	if r.currentState.LastAction.Intent.Action != models.ActionUpgrade {
		t.Errorf("LastAction.Intent.Action: got %q, want %q",
			r.currentState.LastAction.Intent.Action, models.ActionUpgrade)
	}
}

// TestNewNodeHandlerBase_NilDependencies verifies that nil dependencies are
// rejected at construction time rather than causing a nil-pointer panic later.
func TestNewNodeHandlerBase_NilReader(t *testing.T) {
	_, w := newTestPair()
	_, err := NewNodeHandlerBase(nil, w, &rsl.Registry{})
	if err == nil {
		t.Fatal("expected error for nil reader")
	}
}

func TestNewNodeHandlerBase_NilWriter(t *testing.T) {
	r, _ := newTestPair()
	_, err := NewNodeHandlerBase(r, nil, &rsl.Registry{})
	if err == nil {
		t.Fatal("expected error for nil writer")
	}
}

func TestNewNodeHandlerBase_NilRegistry(t *testing.T) {
	r, w := newTestPair()
	_, err := NewNodeHandlerBase(r, w, nil)
	if err == nil {
		t.Fatal("expected error for nil registry")
	}
}

func TestNewNodeHandlerBase_AllValid(t *testing.T) {
	r, w := newTestPair()
	base, err := NewNodeHandlerBase(r, w, &rsl.Registry{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if base.StateReader == nil || base.StateWriter == nil || base.RSL == nil {
		t.Fatal("expected all fields to be set")
	}
}
