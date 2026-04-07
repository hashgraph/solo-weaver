// SPDX-License-Identifier: Apache-2.0

// Package rsl (Runtime State Layer) bridges live cluster reality, persisted
// application state, and operator-supplied inputs into a single, deterministic
// effective value for every configurable field.
//
// # Core types
//
// [Selector] is the selection algorithm interface.  A Selector receives the
// full sources map and returns the winning value together with the strategy
// that produced it.  [DefaultSelector] provides the standard priority-walk
// implementation; custom Selectors encode field-specific rules such as
// validation, soft fall-through, or intent-aware logic.
//
// [EffectiveValue] is the per-field container.  It holds a sources map (keyed
// by [automa.EffectiveStrategy]), a [Selector], and a lazily-computed cached
// result.  Callers push values in via [EffectiveValue.SetSource] and pull the
// winner out via [EffectiveValue.Get] or [EffectiveValue.Resolve].
//
// # Resolution precedence (highest → lowest)
//
//	StrategyReality   — live cluster state  (from RefreshState)
//	StrategyState     — persisted state.yaml (from WithState)
//	StrategyUserInput — CLI flags            (from WithUserInputs)
//	StrategyEnv       — environment variable (set externally)
//	StrategyConfig    — config file          (from WithConfig)
//	StrategyDefault   — hardcoded deps.*     (seeded at construction)
//	StrategyZero      — zero value           (ultimate fallback)
//
// # Lifecycle
//
//  1. Construct with [NewEffectiveValue], passing a [Selector].  The sources map
//     starts empty; call [EffectiveValue.SetSource] (or the With* methods on the
//     owning resolver) to populate sources before resolving.
//  2. Call [EffectiveValue.SetSource] / [EffectiveValue.ClearSource] as inputs
//     arrive (config load, user flags, state refresh).  Each mutation
//     invalidates the cache.
//  3. Call [EffectiveValue.Resolve] (or [EffectiveValue.Get]) to obtain the
//     winning value.  The result is cached until the next mutation.
//  4. Use the source-specific accessors ([EffectiveValue.StateVal],
//     [EffectiveValue.UserInputVal], etc.) to inspect individual layers without
//     triggering resolution.
package rsl

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/automa-saga/automa"
)

// RSL-specific strategy constants extend automa.EffectiveStrategy using values
// 100–106 to avoid conflicts with automa's built-in range (0–4).
//
// Precedence order (highest → lowest):
//
//	StrategyReality (105) > StrategyState (104) > StrategyUserInput (106) >
//	StrategyEnv (102) > StrategyConfig (103) > StrategyDefault (101) >
//	StrategyZero (100)
//
// Note: numeric values do not encode precedence — the order is defined
// explicitly in [defaultOrderedStrategies].
const (
	// StrategyZero is the ultimate fallback returned when no source supplies a
	// value.  The associated value is always the zero value of T.
	StrategyZero automa.EffectiveStrategy = 100

	// StrategyDefault indicates the value came from a hardcoded compile-time
	// constant (e.g. deps.BLOCK_NODE_NAMESPACE).  Set via WithDefaults and
	// typically never changes after initial wiring.
	StrategyDefault automa.EffectiveStrategy = 101

	// StrategyEnv indicates the value came from an environment variable
	// (e.g. SOLO_PROVISIONER_BLOCKNODE_NAMESPACE).
	StrategyEnv automa.EffectiveStrategy = 102

	// StrategyConfig indicates the value came from the config file loaded at
	// startup (e.g. config.yaml).
	StrategyConfig automa.EffectiveStrategy = 103

	// StrategyState indicates the value came from persisted application state
	// (state.yaml on disk).  Set by WithState.
	StrategyState automa.EffectiveStrategy = 104

	// StrategyReality indicates the value came from a live cluster query via
	// the reality checker.  Set by RefreshState.  Takes precedence over
	// StrategyState because it reflects the cluster's actual current condition.
	StrategyReality automa.EffectiveStrategy = 105

	// StrategyUserInput indicates the value was supplied explicitly by the
	// operator via a CLI flag.  Takes precedence over all configuration and
	// environment sources, but not over live cluster reality or persisted state.
	StrategyUserInput automa.EffectiveStrategy = 106
)

// defaultOrderedStrategies is the precedence list used by [DefaultSelector].
// Strategies listed earlier win over those listed later.
var defaultOrderedStrategies = []automa.EffectiveStrategy{
	StrategyReality,
	StrategyState,
	StrategyUserInput,
	StrategyEnv,
	StrategyConfig,
	StrategyDefault,
	StrategyZero,
}

// Selector is the algorithm that picks the winning value from a sources map.
//
// Implementations receive the full sources map — keyed by strategy — and
// return the winning [automa.EffectiveValue] together with any error.  An
// error signals that resolution is impossible given the current sources (e.g.
// a required field is empty in a deployed release).
//
// Use [DefaultSelector] for the standard priority-walk behaviour.  Implement
// this interface directly when a field requires domain-specific rules such as:
//   - Validating that a deployed value is non-empty (e.g. namespace).
//   - Soft fall-through on an empty deployed value with a warning (e.g. chart ref).
//   - Intent-aware ordering where upgrade and non-upgrade actions resolve
//     differently (e.g. chart version).
//   - Post-resolution merging of values from multiple sources (e.g. storage
//     BasePath merge from config into deployed state).
type Selector[T any] interface {
	Resolve(sources map[automa.EffectiveStrategy]automa.Value[T]) (*automa.EffectiveValue[T], error)
}

// DefaultSelector is the standard [Selector] implementation.  It walks
// [defaultOrderedStrategies] in order and returns the first source whose key
// exists in the map, regardless of the value.  If no source is present it
// returns a zero value tagged with [StrategyZero].
type DefaultSelector[T any] struct{}

// Resolve implements [Selector] using the default priority-walk over
// [defaultOrderedStrategies].
func (dfr *DefaultSelector[T]) Resolve(sources map[automa.EffectiveStrategy]automa.Value[T]) (*automa.EffectiveValue[T], error) {
	var zero T

	for _, st := range defaultOrderedStrategies {
		if v, ok := sources[st]; ok {
			return automa.NewEffectiveValue(v, st)
		}
	}

	return automa.NewEffective(zero, StrategyZero)
}

// EffectiveValue holds all value sources for a single configurable field
// together with the [Selector] that picks the winner.
//
// # Sources
//
// Each source is stored in a map keyed by [automa.EffectiveStrategy].  Sources
// are populated by the owner (e.g. [BlockNodeRuntimeResolver]) via
// [EffectiveValue.SetSource] and removed via [EffectiveValue.ClearSource].
// Multiple sources can coexist; the [Selector] decides which one wins.
//
// # Lazy resolution and caching
//
// Resolution is deferred until the first call to [EffectiveValue.Resolve],
// [EffectiveValue.Get], [EffectiveValue.Strategy], or [EffectiveValue.Err].
// Both the result and any error are cached so subsequent calls are free.  Any
// call to [EffectiveValue.SetSource], [EffectiveValue.ClearSource], or
// [EffectiveValue.Invalidate] clears the cache and forces recomputation on the
// next access.
//
// # Source inspection
//
// Callers can inspect individual source layers at any time — before or after
// resolution — using the typed accessors: [EffectiveValue.RealityVal],
// [EffectiveValue.StateVal], [EffectiveValue.UserInputVal],
// [EffectiveValue.EnvVal], [EffectiveValue.ConfigVal],
// [EffectiveValue.DefaultVal].  These never trigger resolution and never
// return errors.
type EffectiveValue[T any] struct {
	sources    map[automa.EffectiveStrategy]automa.Value[T] // per-strategy value sources
	selector   Selector[T]                                  // algorithm that picks the winner
	val        *automa.EffectiveValue[T]                    // cached winning result (nil = not yet computed)
	resolveErr error                                        // cached resolution error  (nil = not yet computed or no error)
}

// Resolve computes the effective value by invoking the [Selector] with the
// current sources map.  Both the winning value and any error are cached after
// the first call; subsequent calls return the cached result without
// recomputing.
//
// To force recomputation after mutating sources, call [EffectiveValue.Invalidate]
// or use [EffectiveValue.SetSource] / [EffectiveValue.ClearSource], which
// invalidate the cache automatically.
func (e *EffectiveValue[T]) Resolve() (*automa.EffectiveValue[T], error) {
	if e.val == nil && e.resolveErr == nil {
		e.val, e.resolveErr = e.selector.Resolve(e.sources)
	}
	return e.val, e.resolveErr
}

// Invalidate clears the cached result and cached error so the next call to
// [EffectiveValue.Resolve] (or any lazy accessor) recomputes from the current
// sources.  It is called automatically by [EffectiveValue.SetSource] and
// [EffectiveValue.ClearSource]; call it directly only when the owning struct
// has mutated external state that the [Selector] closes over (e.g. intent
// change in [chartVersionResolver]).
func (e *EffectiveValue[T]) Invalidate() {
	e.val = nil
	e.resolveErr = nil
}

// Get returns the winning [automa.Value][T] by lazily triggering resolution.
// If resolution fails it returns [EffectiveValue.ZeroVal] so the call is
// always safe — it never panics.
//
// This method is a drop-in replacement for [automa.EffectiveValue.Get].
// Callers that need the error should use [EffectiveValue.Resolve] or
// [EffectiveValue.Err] instead.
func (e *EffectiveValue[T]) Get() automa.Value[T] {
	val, _ := e.Resolve()
	if val == nil {
		return e.ZeroVal()
	}
	return val.Get()
}

// Strategy returns the [automa.EffectiveStrategy] of the winning source by
// lazily triggering resolution.  Returns [StrategyZero] if resolution fails.
//
// This method is a drop-in replacement for [automa.EffectiveValue.Strategy].
// Note: because RSL strategy constants (100–106) are outside automa's known
// range, calling .String() on the returned value produces "unknown".  Use
// [EffectiveValue.StrategyName] for a human-readable label instead.
func (e *EffectiveValue[T]) Strategy() automa.EffectiveStrategy {
	val, _ := e.Resolve()
	if val == nil {
		return StrategyZero
	}
	return val.Strategy()
}

// StrategyName returns the human-readable label for the winning strategy
// (e.g. "reality", "state", "userInput").  Unlike calling Strategy().String()
// directly — which returns "unknown" for RSL-specific constants — this method
// correctly maps the full range 100–106 via the package-level [StrategyName]
// function.
func (e *EffectiveValue[T]) StrategyName() string {
	return StrategyName(e.Strategy())
}

// Err returns the cached resolution error, triggering resolution if it has not
// yet been computed.  Returns nil when resolution succeeded or has not been
// attempted yet and there is no cached error.
func (e *EffectiveValue[T]) Err() error {
	_, err := e.Resolve()
	return err
}

// SetSource registers v as the value for the given strategy and invalidates
// the cached result.  If v cannot be encoded by gob (required by automa's
// Value internals) an error is returned and the sources map is not mutated.
func (e *EffectiveValue[T]) SetSource(strategy automa.EffectiveStrategy, v T) error {
	val, err := automa.NewValue(v)
	if err != nil {
		return err
	}
	e.sources[strategy] = val
	e.Invalidate()
	return nil
}

// ClearSource removes the value for the given strategy from the sources map
// and invalidates the cached result.  It is a no-op if the strategy was not
// present.
func (e *EffectiveValue[T]) ClearSource(strategy automa.EffectiveStrategy) {
	delete(e.sources, strategy)
	e.Invalidate()
}

// StrategyName returns the human-readable label for any [automa.EffectiveStrategy]
// value, including RSL-specific constants (100–106) that automa's own
// String() method does not know about.  For automa built-in strategies the
// result matches automa's own String() output.
func StrategyName(s automa.EffectiveStrategy) string {
	switch s {
	case StrategyZero:
		return "zero"
	case StrategyDefault:
		return "default"
	case StrategyEnv:
		return "env"
	case StrategyConfig:
		return "config"
	case StrategyState:
		return "state"
	case StrategyReality:
		return "reality"
	case StrategyUserInput:
		return "userInput"
	default:
		return s.String() // delegate to automa for built-in strategies
	}
}

// ZeroVal returns the zero value of T wrapped as an [automa.Value][T].
// It is used as a safe fallback when no source is available or resolution fails.
func (e *EffectiveValue[T]) ZeroVal() automa.Value[T] {
	var zero T
	v, _ := automa.NewValue(zero)
	return v
}

// ValOf returns the raw [automa.Value][T] registered for sourceStrategy without
// triggering resolution.  If the strategy has no registered source it returns
// [EffectiveValue.ZeroVal].  Use the typed accessors ([EffectiveValue.StateVal],
// [EffectiveValue.UserInputVal], etc.) for self-documenting call sites.
func (e *EffectiveValue[T]) ValOf(sourceStrategy automa.EffectiveStrategy) automa.Value[T] {
	if v, ok := e.sources[sourceStrategy]; ok {
		return v
	}
	return e.ZeroVal()
}

// DefaultVal returns the value registered under [StrategyDefault] (the
// hardcoded compile-time constant) without triggering resolution.
// Returns [EffectiveValue.ZeroVal] if no default source has been registered.
func (e *EffectiveValue[T]) DefaultVal() (automa.Value[T], error) {
	return e.ValOf(StrategyDefault), nil
}

// UserInputVal returns the value registered under [StrategyUserInput] (a CLI
// flag) without triggering resolution.
// Returns [EffectiveValue.ZeroVal] if the user did not supply this field.
func (e *EffectiveValue[T]) UserInputVal() (automa.Value[T], error) {
	return e.ValOf(StrategyUserInput), nil
}

// EnvVal returns the value registered under [StrategyEnv] (an environment
// variable) without triggering resolution.
// Returns [EffectiveValue.ZeroVal] if no env source has been registered.
func (e *EffectiveValue[T]) EnvVal() (automa.Value[T], error) {
	return e.ValOf(StrategyEnv), nil
}

// ConfigVal returns the value registered under [StrategyConfig] (the config
// file loaded at startup) without triggering resolution.
// Returns [EffectiveValue.ZeroVal] if no config source has been registered.
func (e *EffectiveValue[T]) ConfigVal() (automa.Value[T], error) {
	return e.ValOf(StrategyConfig), nil
}

// StateVal returns the value registered under [StrategyState] (persisted
// state.yaml) without triggering resolution.
// Returns [EffectiveValue.ZeroVal] if the field has no persisted state.
func (e *EffectiveValue[T]) StateVal() (automa.Value[T], error) {
	return e.ValOf(StrategyState), nil
}

// RealityVal returns the value registered under [StrategyReality] (the live
// cluster query result) without triggering resolution.
// Returns [EffectiveValue.ZeroVal] if a reality refresh has not yet been
// performed or the release is not deployed.
func (e *EffectiveValue[T]) RealityVal() (automa.Value[T], error) {
	return e.ValOf(StrategyReality), nil
}

// NewEffectiveValue constructs an [EffectiveValue] for a single field with an
// empty sources map.  Callers populate sources via [EffectiveValue.SetSource] or
// the owning resolver's With* methods (WithDefaults, WithConfig, WithEnv, …).
// selector is the [Selector] invoked on the first call to [EffectiveValue.Resolve].
// If selector is nil, [DefaultSelector] is used.
func NewEffectiveValue[T any](selector Selector[T]) (*EffectiveValue[T], error) {
	if selector == nil {
		selector = &DefaultSelector[T]{}
	}
	return &EffectiveValue[T]{
		sources:  make(map[automa.EffectiveStrategy]automa.Value[T]),
		selector: selector,
	}, nil
}

// WithSelector replaces the current [Selector] and invalidates the cache so the
// next call to [EffectiveValue.Resolve] uses the new algorithm.
func (e *EffectiveValue[T]) WithSelector(selector Selector[T]) {
	if selector == nil {
		selector = &DefaultSelector[T]{}
	}
	e.selector = selector
	e.Invalidate()
}

// String returns a human-readable one-liner summary of the effective value,
// intended for console output, fmt.Sprintf, and error messages.
// For structured logging use logx.Any() — which calls MarshalJSON().
//
// Format:
//
//	strategy=<name> value=<val> sources=[<name>:<val>, …]
//
// Sources are listed in precedence order (highest → lowest), skipping
// [StrategyZero] since it is the implicit fallback and never stored.
// Resolution is triggered lazily if not yet cached; errors are surfaced as
// value=<error: …>.
func (e *EffectiveValue[T]) String() string {
	// Resolve (uses cache if already computed).
	ev, err := e.Resolve()

	var winVal, winStrategy string
	if err != nil {
		winVal = fmt.Sprintf("<error: %v>", err)
		winStrategy = StrategyName(StrategyZero)
	} else if ev == nil {
		winVal = "<nil>"
		winStrategy = StrategyName(StrategyZero)
	} else {
		winVal = fmt.Sprintf("%v", ev.Get().Val())
		winStrategy = StrategyName(ev.Strategy())
	}

	// Build ordered sources list, skipping StrategyZero (implicit, never stored).
	var parts []string
	for _, st := range defaultOrderedStrategies {
		if st == StrategyZero {
			continue
		}
		if v, ok := e.sources[st]; ok {
			parts = append(parts, fmt.Sprintf("%s:%v", StrategyName(st), v.Val()))
		}
	}

	sources := "[]"
	if len(parts) > 0 {
		sources = "[" + strings.Join(parts, ", ") + "]"
	}

	return fmt.Sprintf("strategy=%s value=%v sources=%s", winStrategy, winVal, sources)
}

// jsonEffectiveValue is the JSON wire representation of [EffectiveValue].
type jsonEffectiveValue struct {
	Strategy string         `json:"strategy"`
	Value    any            `json:"value"`
	Sources  map[string]any `json:"sources"`
}

// MarshalJSON implements json.Marshaler, emitting a structured JSON object so
// that zerolog's Any() and other JSON encoders produce a queryable log field
// rather than a plain text string or an empty "{}".
//
// Output shape:
//
//	{"strategy":"<name>","value":<val>,"sources":{"<name>":<val>,…}}
//
// "strategy" and "value" reflect the winning source.  "sources" contains every
// registered (non-zero) source keyed by strategy name.  Resolution errors are
// surfaced as the "value" string "<error: …>" and strategy "zero".
func (e *EffectiveValue[T]) MarshalJSON() ([]byte, error) {
	ev, resolveErr := e.Resolve()

	var winVal any
	var winStrategy string
	if resolveErr != nil {
		winVal = fmt.Sprintf("<error: %v>", resolveErr)
		winStrategy = StrategyName(StrategyZero)
	} else if ev == nil {
		winVal = nil
		winStrategy = StrategyName(StrategyZero)
	} else {
		winVal = ev.Get().Val()
		winStrategy = StrategyName(ev.Strategy())
	}

	sources := make(map[string]any, len(e.sources))
	for _, st := range defaultOrderedStrategies {
		if st == StrategyZero {
			continue
		}
		if v, ok := e.sources[st]; ok {
			sources[StrategyName(st)] = v.Val()
		}
	}

	return json.Marshal(jsonEffectiveValue{
		Strategy: winStrategy,
		Value:    winVal,
		Sources:  sources,
	})
}
