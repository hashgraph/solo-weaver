// SPDX-License-Identifier: Apache-2.0

package bll

// Package bll (business logic layer) owns intent routing, effective-value
// resolution, and workflow orchestration.  It sits between the CLI commands
// (cmd layer) and the workflow / step layer.
//
// Shared infrastructure lives in BaseHandler so that every per-node-type
// handler (BlockNodeRuntimeResolver, MirrorNode, RelayNode, …) inherits it without duplication.

import (
	"context"
	"slices"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/drift"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
)

// ── BaseHandler ───────────────────────────────────────────────────────────

// BaseHandler holds the three dependencies that every node-type handler
// needs: a StateManager for reading and writing state, the rsl Runtime, and
// the reality Checker for live machine-state queries.
//
// Embed this struct in any concrete handler to inherit HandleIntent, with its
// refresh and flush, without re-implementing them.
//
//	type BlockNodeHandler struct {
//	    BaseHandler
//	    install   *BlockNodeInstallHandler
//	    upgrade   *BlockNodeUpgradeHandler
//	    reset     *BlockNodeResetHandler
//	    uninstall *BlockNodeUninstallHandler
//	}
type BaseHandler[T any] struct {
	Runtime          *rsl.RuntimeResolver
	Target           models.TargetType // expected target for intent validation
	ProfileExtractor func(T) string    // optional; extracts profile from custom inputs (nil for handlers without profile)
	Managed          []Component       // components this handler owns; scopes drift detection and the state flush (none by default)
	// LockWait bounds how long HandleIntent blocks acquiring a managed
	// component's lock before failing. Zero (the default) fails immediately
	// rather than waiting — see WithLockWait.
	LockWait time.Duration
	// KeptAtBaseline, when set, puts back the parts of a managed component this
	// handler doesn't own, as they were on disk, before the flush, and
	// KeptChange reports the drift on those parts — see WithKeptAtBaseline.
	KeptAtBaseline func(live *state.State, baseline state.State)
	KeptChange     func(drift.Change) bool
}

// BaseHandlerOption is a functional option for configuring a BaseHandler.
type BaseHandlerOption[T any] func(*BaseHandler[T])

// WithProfileExtractor returns a BaseHandlerOption that sets the profile extractor function.
// The extractor is called during flushState to persist the deployment profile into
// MachineState.Profile, so the handler must also manage Profile.  Handlers whose input
// types carry no profile (e.g. teleport) should omit this option — the extractor
// defaults to nil, and no profile is written.
func WithProfileExtractor[T any](fn func(T) string) BaseHandlerOption[T] {
	return func(h *BaseHandler[T]) {
		h.ProfileExtractor = fn
	}
}

// WithManagedComponents returns a BaseHandlerOption that declares the components a
// handler owns. The one declaration scopes drift detection (only managed components'
// producers run), the locks HandleIntent holds for the whole command, and the state
// flush (only managed components' files are written). Handlers that omit it manage
// nothing: they report no drift, lock nothing and write no component file.
func WithManagedComponents[T any](components ...Component) BaseHandlerOption[T] {
	return func(h *BaseHandler[T]) {
		h.Managed = components
	}
}

// WithKeptAtBaseline returns a BaseHandlerOption for a handler that shares a
// managed component but owns only part of it, such as either Teleport agent.
// Before the flush, keep copies the part it doesn't own from the on-disk
// baseline into the live state, so the handler never records an out-of-band
// change to it, and that part's own command still sees the drift. kept reports
// the drift on that part, which the run neither guards nor records.
func WithKeptAtBaseline[T any](
	keep func(live *state.State, baseline state.State),
	kept func(drift.Change) bool,
) BaseHandlerOption[T] {
	return func(h *BaseHandler[T]) {
		h.KeptAtBaseline = keep
		h.KeptChange = kept
	}
}

// WithLockWait returns a BaseHandlerOption that sets how long HandleIntent
// blocks acquiring a managed component's lock before failing. The default
// (unset, zero) fails immediately — see BaseHandler.LockWait.
func WithLockWait[T any](wait time.Duration) BaseHandlerOption[T] {
	return func(h *BaseHandler[T]) {
		h.LockWait = wait
	}
}

// ownedComponentIDs returns the internal/state.ComponentID of every managed
// component: the files this handler locks for the whole command, then writes.
func (h *BaseHandler[T]) ownedComponentIDs() []state.ComponentID {
	return state.DedupeComponentIDs(componentIDsOf(h.Managed))
}

// Owns reports whether c is one of the handler's managed components.
func (h *BaseHandler[T]) Owns(c Component) bool {
	return slices.Contains(componentIDsOf(h.Managed), c.id)
}

// NewBaseHandler validates the required dependencies and returns a
// populated BaseHandler.  All fields are required; any nil returns an error.
func NewBaseHandler[T any](reg *rsl.RuntimeResolver, target models.TargetType, opts ...BaseHandlerOption[T]) (BaseHandler[T], error) {
	if reg == nil {
		return BaseHandler[T]{}, errorx.IllegalArgument.New("RuntimeResolver cannot be nil")
	}
	h := BaseHandler[T]{Runtime: reg, Target: target}
	for _, opt := range opts {
		opt(&h)
	}
	// The extractor writes MachineState.Profile, which only reaches disk when
	// the handler flushes the profile file.
	if h.ProfileExtractor != nil && !h.Owns(Profile) {
		return BaseHandler[T]{}, errx.WithReason(
			errorx.AssertionFailed.New("a handler with a profile extractor must manage the profile component"),
			reasons.Internal)
	}
	return h, nil
}

func (h *BaseHandler[T]) ValidateIntent(intent models.Intent, inputs models.UserInputs[T], target models.TargetType) error {
	// ── 1. Validate intent ─────────────────────────────────────────────────
	if !intent.IsValid() {
		return errorx.IllegalArgument.New("invalid intent: %v", intent)
	}

	if intent.Target != target {
		return errorx.IllegalArgument.New("intent target mismatch: expected %s, got %s", target, intent.Target)
	}

	if err := inputs.Validate(); err != nil {
		return errorx.IllegalArgument.New("invalid user inputs: %v", err)
	}

	return nil
}

// HandleIntent is the shared generic handler.
// It performs the following steps:
//  1. Validates the intent and user inputs.
//  2. Locks every component this handler manages for the rest of the call, so
//     no other command writing the same component can run concurrently.
//  3. Refreshes the runtime state to ensure it's up-to-date before workflow execution, noting fields
//     whose live value differs from the persisted state.
//  4. Delegates to the per-action handler to prepare effective inputs and build the workflow, then executes it,
//     unless the action would destroy owned drift (see guardOwnedDrift).
//  5. Flushes the updated state to disk, performing three live refreshes before persistence.
//
// The callback parameter is an optional function that allows per-action handlers to apply additional mutations to the
// full state before it is flushed to disk.
func (h *BaseHandler[T]) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[T],
	ac IntentHandler[T],
	callback func(full *state.State, effInputs models.UserInputs[T]) error, // optional callback for applying additional mutations to the full state
) (*automa.Report, error) {
	// ── 1. Validate intent and inputs ───────────────────────────────────────────────
	err := h.ValidateIntent(intent, inputs, h.Target)
	if err != nil {
		return nil, err
	}

	// Lock every managed component for the whole command, not just the flush
	// — see internal/state.AcquireComponentLocks.
	release, err := state.AcquireComponentLocks(h.Runtime.StateDir(), h.ownedComponentIDs(), h.LockWait)
	if err != nil {
		return nil, err
	}
	defer release()

	// ── 2. Refresh runtime state ───────────────────────────────────────────────
	// We need to refresh runtime before preparing effective inputs
	baseline, currentState, err := h.Runtime.RefreshWithBaseline(ctx, true)
	if err != nil {
		return nil, err
	}
	// Only this refresh can see out-of-band changes: by the flush, reality also
	// holds the workflow's own changes.
	outOfBand := detectOutOfBandChanges(baseline, currentState, producersOf(h.Managed)...)

	// ── 3. Prepare effective inputs ───────────────────────────────────────────────
	effectiveInputs, err := ac.PrepareEffectiveInputs(intent, inputs)
	if err != nil {
		return nil, err
	}

	wb, err := ac.BuildWorkflow(currentState, *effectiveInputs)
	if err != nil {
		return nil, err
	}

	// ── 4. Execute workflow ────────────────────────────────────────────────
	wf, err := wb.Build()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to build workflow: %v", err)
	}

	// After the action's own preconditions, so a run that would fail anyway is
	// not told to override.
	destroys := func(drift.Change) bool { return false }
	if d, ok := ac.(DriftDestroyer[T]); ok {
		destroys = func(c drift.Change) bool { return d.DestroysDrift(*effectiveInputs, c) }
	}
	// Drift on a part the handler keeps at baseline isn't this run's to write.
	owned := outOfBand
	if h.KeptChange != nil {
		owned = slices.DeleteFunc(slices.Clone(outOfBand), h.KeptChange)
	}
	driftRecords, unverified, err := guardOwnedDrift(intent, effectiveInputs.Common, destroys, owned)
	if err != nil {
		return nil, errx.Decorate(err, reasons.PreconditionNotMet, driftGuardHint(h.Runtime.StateDir(), unverified))
	}

	logx.As().Info().
		Any("intent", intent).
		Any("inputs", inputs.Redacted()).
		Any("effectiveInputs", effectiveInputs.Redacted()).
		Msgf("Running %s workflow for intent %q", intent.Target, intent.Action)

	report := wf.Execute(ctx)
	addOutOfBandWarnings(report, outOfBand)

	// ── 5. Flush state ─────────────────────────────────────────────────────
	return h.flushState(
		ctx,
		report,
		intent,
		effectiveInputs,
		ownedDriftRun{records: driftRecords, baseline: baseline, destroys: destroys},
		callback,
	)
}

// flushState is the generic flush after a run's workflow. owned is the owned
// drift the run went ahead over: the flush's refresh confirms which records
// were destroyed, for the action history entry.
func (h *BaseHandler[T]) flushState(
	ctx context.Context,
	report *automa.Report,
	intent models.Intent,
	effectiveInputs *models.UserInputs[T],
	owned ownedDriftRun,
	callback func(full *state.State, effInputs models.UserInputs[T]) error,
) (*automa.Report, error) {
	if report == nil {
		return nil, errorx.IllegalArgument.New("workflow report cannot be nil")
	}

	baseline, fullState, err := h.Runtime.RefreshWithBaseline(ctx, true)
	if err != nil {
		return nil, errorx.IllegalState.New("failed to refresh runtime state before flush: %v", err)
	}

	h.Runtime.AddActionHistory(state.ActionHistory{
		Intent: intent,
		Inputs: effectiveInputs,
		// Provenance: the component files this flush writes, so action_history.yaml
		// records what on disk each action changed (see state.ActionHistory.Components).
		Components: h.ownedComponentIDs(),
		OwnedDrift: owned.confirm(fullState, producersOf(h.Managed)...),
	})
	if h.KeptAtBaseline != nil && baseline != nil {
		h.KeptAtBaseline(&fullState, *baseline)
	}

	// Persist the deployment profile when the handler carries one.
	// This is done before the per-action callback so that callback logic can
	// read/override the profile if needed.
	if h.ProfileExtractor != nil && effectiveInputs != nil {
		if profile := h.ProfileExtractor(effectiveInputs.Custom); profile != "" {
			fullState.MachineState.Profile = profile
			logx.As().Debug().Str("profile", profile).Msg("Persisted deployment profile into state")
		}
	}

	if callback != nil {
		logx.As().Debug().Msg("Applying callback mutations to full state before flush")
		if err := callback(&fullState, *effectiveInputs); err != nil {
			return nil, errorx.IllegalState.New("failed to patch state after workflow execution: %v", err)
		}
		logx.As().Debug().Any("fullState", fullState).Msg("State after applying callback mutations")
	}

	// Flush only the files this handler owns, plus the action history.
	if err := h.Runtime.FlushScoped(fullState, h.ownedComponentIDs()...); err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to persist state after workflow")
	}

	logx.As().Info().
		Str("state_file", fullState.StateFile).
		Any("full-state", fullState).
		Msg("Persisted state after workflow execution")

	return report, nil
}
