// SPDX-License-Identifier: Apache-2.0

// Package resolver implements pure, side-effect-free value resolution.
//
// Given three sources — a config default, the current persisted/live state, and
// an optional user input — it returns the single effective value together with
// the strategy that selected it.  No I/O, no network calls, no locks.
//
// # Resolution priority (highest wins)
//
//	StrategyCurrent   — the resource is already deployed; current state wins.
//	StrategyUserInput — the caller explicitly supplied a non-empty value.
//	StrategyConfig    — fall back to the configured default.
//
// # Extending with custom logic
//
// Three layers of customisation are available:
//
//  1. [WithFunc] — fully generic; supply your own isDeployed, equal, and isEmpty
//     callbacks.  Use this when field-level semantics differ from the default
//     (e.g. a struct field with a non-zero "empty" sentinel, or a field whose
//     emptiness is context-dependent).
//
//  2. [ForStatus] — convenience wrapper that derives isDeployed from a Helm
//     release.Status.  Covers the common case for all simple scalar fields.
//
//  3. [Field] — combines selection with an optional post-selection Validator.
//     Register a Validator to enforce immutability rules, cross-field
//     constraints, or any other domain rule that must hold after the winning
//     value is chosen.  This keeps validation co-located with resolution
//     rather than scattered across the BLL.
//
// Because this package contains no I/O, every function and type is
// unit-testable without mocks.
package resolver

import (
	"reflect"

	"github.com/automa-saga/automa"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
)

// ── Core selection ────────────────────────────────────────────────────────────

// WithFunc computes the effective value for a single field.
//
// Parameters:
//   - defaultVal  – config default wrapped in automa.Value[T].
//   - userInput   – value provided by the user; may be nil or the zero value of T.
//   - currentVal  – value read from persisted or live state.
//   - isDeployed  – callback that returns true when the resource already exists.
//   - equal       – optional equality predicate; defaults to reflect.DeepEqual.
//   - isEmpty     – optional emptiness check; defaults to reflect.Value.IsZero.
func WithFunc[T any](
	defaultVal automa.Value[T],
	userInput automa.Value[T],
	currentVal T,
	isDeployed func() bool,
	equal func(a, b T) bool,
	isEmpty func(v T) bool,
) (*automa.EffectiveValue[T], error) {
	if equal == nil {
		equal = func(a, b T) bool { return reflect.DeepEqual(a, b) }
	}
	if isEmpty == nil {
		isEmpty = func(v T) bool { return reflect.ValueOf(v).IsZero() }
	}

	val := defaultVal.Val()
	strategy := automa.StrategyConfig

	if isDeployed() {
		val = currentVal
		strategy = automa.StrategyCurrent
	} else if userInput != nil && !isEmpty(userInput.Val()) {
		val = userInput.Val()
		strategy = automa.StrategyUserInput
	}

	return automa.NewEffective(val, strategy)
}

// ForStatus is a convenience wrapper around WithFunc that derives isDeployed
// from a Helm release.Status value.
//
// cacheResult is returned as-is so callers can forward it unchanged to
// automa's WithEffectiveFunc three-value return signature.
func ForStatus[T any](
	defaultVal automa.Value[T],
	userInput automa.Value[T],
	currentVal T,
	status release.Status,
	cacheResult bool,
) (*automa.EffectiveValue[T], bool, error) {
	ev, err := WithFunc[T](
		defaultVal,
		userInput,
		currentVal,
		func() bool { return status == release.StatusDeployed },
		nil,
		nil,
	)
	return ev, cacheResult, err
}

// ── Validation ────────────────────────────────────────────────────────────────

// Validator is a constraint applied after value selection.
// It receives the resolved EffectiveValue and returns an error if the
// selection violates a domain rule (e.g. immutability, cross-field constraint).
//
// Implementations must be pure — no I/O or side effects.
type Validator[T any] func(ev *automa.EffectiveValue[T]) error

// ImmutableOnDeploy returns a Validator that rejects any attempt to change
// a field once a resource is deployed.  It is the most common constraint for
// fields like namespace and release name.
func ImmutableOnDeploy[T any](fieldName string) Validator[T] {
	return func(ev *automa.EffectiveValue[T]) error {
		if ev.Strategy() == automa.StrategyCurrent {
			// value came from current state — no change attempted, always OK.
			return nil
		}
		// StrategyUserInput while deployed means the user tried to change it.
		if ev.Strategy() == automa.StrategyUserInput {
			return errorx.IllegalArgument.New(
				"field %q is immutable once deployed; the existing value cannot be changed",
				fieldName,
			)
		}
		return nil
	}
}

// ── Field — selection + validation composed ───────────────────────────────────

// Field combines a selection function with zero or more Validators into a
// single callable that can be passed directly to automa.WithEffectiveFunc.
//
// Using Field keeps resolution and domain constraints co-located.  Adding a
// new constraint for a field is a one-line change at the call site rather than
// a scattered guard inside the BLL.
//
// Example — immutable namespace:
//
//	resolver.Field(
//	    func() (*automa.EffectiveValue[string], error) {
//	        return resolver.WithFunc(def, user, current, isDeployed, nil, nil)
//	    },
//	    resolver.ImmutableOnDeploy[string]("namespace"),
//	)
func Field[T any](
	selectFn func() (*automa.EffectiveValue[T], error),
	validators ...Validator[T],
) (*automa.EffectiveValue[T], error) {
	ev, err := selectFn()
	if err != nil {
		return nil, err
	}
	for _, v := range validators {
		if err = v(ev); err != nil {
			return nil, err
		}
	}
	return ev, nil
}
