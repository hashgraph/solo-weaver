// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package rsl

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Test selectors ────────────────────────────────────────────────────────────

// errorSelector always returns a configurable error, used to test error propagation.
type errorSelector[T any] struct{ err error }

func (e *errorSelector[T]) Resolve(_ map[automa.EffectiveStrategy]automa.Value[T]) (*automa.EffectiveValue[T], error) {
	return nil, e.err
}

// callCountSelector delegates to an inner function and records invocations,
// used to verify caching behaviour.
type callCountSelector[T any] struct {
	calls     int
	onResolve func(map[automa.EffectiveStrategy]automa.Value[T]) (*automa.EffectiveValue[T], error)
}

func (c *callCountSelector[T]) Resolve(sources map[automa.EffectiveStrategy]automa.Value[T]) (*automa.EffectiveValue[T], error) {
	c.calls++
	return c.onResolve(sources)
}

// newCountingDefaultSelector wraps DefaultSelector and counts calls.
func newCountingDefaultSelector[T any]() *callCountSelector[T] {
	return &callCountSelector[T]{
		onResolve: func(sources map[automa.EffectiveStrategy]automa.Value[T]) (*automa.EffectiveValue[T], error) {
			return (&DefaultSelector[T]{}).Resolve(sources)
		},
	}
}

// ── NewEffectiveValue ─────────────────────────────────────────────────────────

func TestNewEffectiveValue_NilSelector_UsesDefaultSelector(t *testing.T) {
	ev, err := NewEffectiveValue[string](nil)
	require.NoError(t, err)
	require.NotNil(t, ev)

	// No sources registered yet → StrategyZero fallback.
	assert.Equal(t, StrategyZero, ev.Strategy())
	assert.Equal(t, "", ev.Get().Val())
}

func TestNewEffectiveValue_CustomSelector(t *testing.T) {
	sel := &DefaultSelector[string]{}
	ev, err := NewEffectiveValue[string](sel)
	require.NoError(t, err)
	require.NotNil(t, ev)
}

func TestNewEffectiveValue_EmptySources_ReturnsStrategyZero(t *testing.T) {
	ev, err := NewEffectiveValue[string](&DefaultSelector[string]{})
	require.NoError(t, err)

	val, resolveErr := ev.Resolve()
	require.NoError(t, resolveErr)
	require.NotNil(t, val)
	assert.Equal(t, StrategyZero, ev.Strategy())
	assert.Equal(t, "", ev.Get().Val())
}

// ── SetSource ─────────────────────────────────────────────────────────────────

func TestEffectiveValue_SetSource_RegistersAndReturnsValue(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&DefaultSelector[string]{})

	require.NoError(t, ev.SetSource(StrategyDefault, "dep-value"))

	assert.Equal(t, "dep-value", ev.Get().Val())
	assert.Equal(t, StrategyDefault, ev.Strategy())
}

func TestEffectiveValue_SetSource_InvalidatesCache(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&DefaultSelector[string]{})
	require.NoError(t, ev.SetSource(StrategyDefault, "first"))

	assert.Equal(t, "first", ev.Get().Val()) // populate cache

	require.NoError(t, ev.SetSource(StrategyDefault, "second"))
	assert.Equal(t, "second", ev.Get().Val()) // cache must have been invalidated
}

func TestEffectiveValue_SetSource_HigherPriorityWins(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&DefaultSelector[string]{})
	require.NoError(t, ev.SetSource(StrategyDefault, "dep-value"))
	require.NoError(t, ev.SetSource(StrategyConfig, "cfg-value"))

	assert.Equal(t, "cfg-value", ev.Get().Val())
	assert.Equal(t, StrategyConfig, ev.Strategy())
}

// ── ClearSource ───────────────────────────────────────────────────────────────

func TestEffectiveValue_ClearSource_RemovesAndFallsBack(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&DefaultSelector[string]{})
	require.NoError(t, ev.SetSource(StrategyDefault, "dep-value"))
	require.NoError(t, ev.SetSource(StrategyConfig, "cfg-value"))

	assert.Equal(t, "cfg-value", ev.Get().Val()) // config wins initially

	ev.ClearSource(StrategyConfig)
	assert.Equal(t, "dep-value", ev.Get().Val()) // falls back to default
	assert.Equal(t, StrategyDefault, ev.Strategy())
}

func TestEffectiveValue_ClearSource_AllSources_YieldsStrategyZero(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&DefaultSelector[string]{})
	require.NoError(t, ev.SetSource(StrategyDefault, "dep"))
	ev.ClearSource(StrategyDefault)

	assert.Equal(t, StrategyZero, ev.Strategy())
	assert.Equal(t, "", ev.Get().Val())
}

func TestEffectiveValue_ClearSource_NoOpWhenNotPresent(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&DefaultSelector[string]{})
	// no panic expected
	ev.ClearSource(StrategyDefault)
	assert.Equal(t, StrategyZero, ev.Strategy())
}

func TestEffectiveValue_ClearSource_InvalidatesCache(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&DefaultSelector[string]{})
	require.NoError(t, ev.SetSource(StrategyConfig, "cfg"))
	assert.Equal(t, "cfg", ev.Get().Val()) // populate cache

	ev.ClearSource(StrategyConfig)
	assert.Equal(t, StrategyZero, ev.Strategy()) // cache cleared, re-resolved
}

// ── DefaultSelector priority walk ────────────────────────────────────────────

func TestDefaultSelector_PriorityWalk(t *testing.T) {
	tests := []struct {
		name         string
		sources      map[automa.EffectiveStrategy]string
		wantVal      string
		wantStrategy automa.EffectiveStrategy
	}{
		{
			name: "reality_beats_all",
			sources: map[automa.EffectiveStrategy]string{
				StrategyReality:   "reality",
				StrategyState:     "state",
				StrategyUserInput: "user",
				StrategyEnv:       "env",
				StrategyConfig:    "config",
				StrategyDefault:   "default",
			},
			wantVal: "reality", wantStrategy: StrategyReality,
		},
		{
			name: "state_beats_userInput_env_config_default",
			sources: map[automa.EffectiveStrategy]string{
				StrategyState:     "state",
				StrategyUserInput: "user",
				StrategyEnv:       "env",
				StrategyConfig:    "config",
				StrategyDefault:   "default",
			},
			wantVal: "state", wantStrategy: StrategyState,
		},
		{
			name: "userInput_beats_env_config_default",
			sources: map[automa.EffectiveStrategy]string{
				StrategyUserInput: "user",
				StrategyEnv:       "env",
				StrategyConfig:    "config",
				StrategyDefault:   "default",
			},
			wantVal: "user", wantStrategy: StrategyUserInput,
		},
		{
			name: "env_beats_config_default",
			sources: map[automa.EffectiveStrategy]string{
				StrategyEnv:     "env",
				StrategyConfig:  "config",
				StrategyDefault: "default",
			},
			wantVal: "env", wantStrategy: StrategyEnv,
		},
		{
			name: "config_beats_default",
			sources: map[automa.EffectiveStrategy]string{
				StrategyConfig:  "config",
				StrategyDefault: "default",
			},
			wantVal: "config", wantStrategy: StrategyConfig,
		},
		{
			name: "default_only",
			sources: map[automa.EffectiveStrategy]string{
				StrategyDefault: "default",
			},
			wantVal: "default", wantStrategy: StrategyDefault,
		},
		{
			name:    "no_sources_yields_zero",
			sources: map[automa.EffectiveStrategy]string{},
			wantVal: "", wantStrategy: StrategyZero,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ev, _ := NewEffectiveValue[string](&DefaultSelector[string]{})
			for strategy, val := range tc.sources {
				require.NoError(t, ev.SetSource(strategy, val))
			}
			assert.Equal(t, tc.wantVal, ev.Get().Val())
			assert.Equal(t, tc.wantStrategy, ev.Strategy())
		})
	}
}

// ── Caching ───────────────────────────────────────────────────────────────────

func TestEffectiveValue_Resolve_CachesResult(t *testing.T) {
	sel := newCountingDefaultSelector[string]()
	ev, _ := NewEffectiveValue[string](sel)
	require.NoError(t, ev.SetSource(StrategyDefault, "v"))

	for i := 0; i < 5; i++ {
		_, _ = ev.Resolve()
	}

	assert.Equal(t, 1, sel.calls, "selector should be invoked only once due to caching")
}

func TestEffectiveValue_Invalidate_ForcesRecomputation(t *testing.T) {
	sel := newCountingDefaultSelector[string]()
	ev, _ := NewEffectiveValue[string](sel)
	require.NoError(t, ev.SetSource(StrategyDefault, "v"))

	_, _ = ev.Resolve()
	assert.Equal(t, 1, sel.calls)

	ev.Invalidate()
	_, _ = ev.Resolve()
	assert.Equal(t, 2, sel.calls, "Invalidate must force re-resolution on next call")
}

func TestEffectiveValue_SetSource_AutomaticallyInvalidatesCache(t *testing.T) {
	sel := newCountingDefaultSelector[string]()
	ev, _ := NewEffectiveValue[string](sel)
	require.NoError(t, ev.SetSource(StrategyDefault, "first"))
	_, _ = ev.Resolve()
	assert.Equal(t, 1, sel.calls)

	// SetSource should invalidate
	require.NoError(t, ev.SetSource(StrategyConfig, "second"))
	_, _ = ev.Resolve()
	assert.Equal(t, 2, sel.calls)
}

// ── Source accessors ──────────────────────────────────────────────────────────

func TestEffectiveValue_SourceAccessors_ReturnCorrectLayerValues(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&DefaultSelector[string]{})
	require.NoError(t, ev.SetSource(StrategyDefault, "def"))
	require.NoError(t, ev.SetSource(StrategyConfig, "cfg"))
	require.NoError(t, ev.SetSource(StrategyEnv, "env"))
	require.NoError(t, ev.SetSource(StrategyUserInput, "user"))
	require.NoError(t, ev.SetSource(StrategyState, "state"))
	require.NoError(t, ev.SetSource(StrategyReality, "reality"))

	tests := []struct {
		name    string
		getVal  func() (automa.Value[string], error)
		wantVal string
	}{
		{"DefaultVal", ev.DefaultVal, "def"},
		{"ConfigVal", ev.ConfigVal, "cfg"},
		{"EnvVal", ev.EnvVal, "env"},
		{"UserInputVal", ev.UserInputVal, "user"},
		{"StateVal", ev.StateVal, "state"},
		{"RealityVal", ev.RealityVal, "reality"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			val, err := tc.getVal()
			require.NoError(t, err)
			assert.Equal(t, tc.wantVal, val.Val())
		})
	}
}

func TestEffectiveValue_SourceAccessors_ReturnZeroWhenAbsent(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&DefaultSelector[string]{})

	dv, _ := ev.DefaultVal()
	assert.Equal(t, "", dv.Val(), "DefaultVal should be zero when not registered")

	uv, _ := ev.UserInputVal()
	assert.Equal(t, "", uv.Val(), "UserInputVal should be zero when not registered")

	rv, _ := ev.RealityVal()
	assert.Equal(t, "", rv.Val(), "RealityVal should be zero when not registered")
}

func TestEffectiveValue_ValOf_UnknownStrategy_ReturnsZero(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&DefaultSelector[string]{})
	// 200 is within uint8 range and is not a registered RSL strategy constant.
	val := ev.ValOf(automa.EffectiveStrategy(200))
	assert.Equal(t, "", val.Val())
}

// ── StrategyName ─────────────────────────────────────────────────────────────

func TestStrategyName_AllStrategiesMapped(t *testing.T) {
	tests := []struct {
		strategy automa.EffectiveStrategy
		want     string
	}{
		{StrategyZero, "zero"},
		{StrategyDefault, "default"},
		{StrategyEnv, "env"},
		{StrategyConfig, "config"},
		{StrategyState, "state"},
		{StrategyReality, "reality"},
		{StrategyUserInput, "userInput"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			assert.Equal(t, tc.want, StrategyName(tc.strategy))
		})
	}
}

func TestEffectiveValue_StrategyName_ReflectsWinningStrategy(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&DefaultSelector[string]{})
	require.NoError(t, ev.SetSource(StrategyDefault, "dep"))
	require.NoError(t, ev.SetSource(StrategyConfig, "cfg"))

	assert.Equal(t, "config", ev.StrategyName(), "should name the winning strategy")
}

func TestEffectiveValue_StrategyName_ZeroWhenNoSources(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&DefaultSelector[string]{})
	assert.Equal(t, "zero", ev.StrategyName())
}

// ── Error propagation ─────────────────────────────────────────────────────────

func TestEffectiveValue_SelectorError_PropagatesFromResolve(t *testing.T) {
	sentinel := errors.New("selector failure")
	ev, _ := NewEffectiveValue[string](&errorSelector[string]{err: sentinel})
	require.NoError(t, ev.SetSource(StrategyDefault, "v"))

	_, err := ev.Resolve()
	require.ErrorIs(t, err, sentinel)
}

func TestEffectiveValue_SelectorError_Err_ReturnsSentinel(t *testing.T) {
	sentinel := errors.New("selector failure")
	ev, _ := NewEffectiveValue[string](&errorSelector[string]{err: sentinel})
	require.NoError(t, ev.SetSource(StrategyDefault, "v"))

	require.ErrorIs(t, ev.Err(), sentinel)
}

func TestEffectiveValue_SelectorError_Strategy_ReturnsZero(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&errorSelector[string]{err: errors.New("boom")})
	require.NoError(t, ev.SetSource(StrategyDefault, "v"))

	assert.Equal(t, StrategyZero, ev.Strategy(), "Strategy() should return StrategyZero on selector error")
}

func TestEffectiveValue_Get_NeverPanicsOnSelectorError(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&errorSelector[string]{err: errors.New("boom")})
	require.NoError(t, ev.SetSource(StrategyDefault, "v"))

	require.NotPanics(t, func() {
		val := ev.Get()
		assert.Equal(t, "", val.Val(), "Get() must return zero value when selector errors")
	})
}

func TestEffectiveValue_SelectorError_CachedAcrossCalls(t *testing.T) {
	sel := &errorSelector[string]{err: errors.New("boom")}
	wrapper := &callCountSelector[string]{
		onResolve: func(sources map[automa.EffectiveStrategy]automa.Value[string]) (*automa.EffectiveValue[string], error) {
			return sel.Resolve(sources)
		},
	}
	ev, _ := NewEffectiveValue[string](wrapper)
	require.NoError(t, ev.SetSource(StrategyDefault, "v"))

	// Multiple Resolve calls should still only invoke selector once (error is cached too).
	_, _ = ev.Resolve()
	_, _ = ev.Resolve()
	_, _ = ev.Resolve()
	assert.Equal(t, 1, wrapper.calls, "error should be cached like a successful result")
}

// ── WithSelector ─────────────────────────────────────────────────────────────

func TestEffectiveValue_WithSelector_ReplacesAlgorithmAndInvalidatesCache(t *testing.T) {
	// Start with DefaultSelector: config beats default.
	ev, _ := NewEffectiveValue[string](&DefaultSelector[string]{})
	require.NoError(t, ev.SetSource(StrategyDefault, "dep"))
	require.NoError(t, ev.SetSource(StrategyConfig, "cfg"))
	assert.Equal(t, "cfg", ev.Get().Val()) // cache populated

	// Switch to an error selector — cache must be invalidated.
	ev.WithSelector(&errorSelector[string]{err: errors.New("new selector")})
	_, err := ev.Resolve()
	require.Error(t, err)
}

func TestEffectiveValue_WithSelector_NilFallsBackToDefault(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&errorSelector[string]{err: errors.New("boom")})
	require.NoError(t, ev.SetSource(StrategyDefault, "v"))

	// Replace with nil → should use DefaultSelector
	ev.WithSelector(nil)
	val, err := ev.Resolve()
	require.NoError(t, err)
	assert.Equal(t, "v", val.Get().Val())
}

// ── String ────────────────────────────────────────────────────────────────────

func TestEffectiveValue_String_Format(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&DefaultSelector[string]{})
	require.NoError(t, ev.SetSource(StrategyDefault, "dep-val"))
	require.NoError(t, ev.SetSource(StrategyConfig, "cfg-val"))

	s := ev.String()
	assert.True(t, strings.Contains(s, "strategy=config"), "must name winning strategy: %s", s)
	assert.True(t, strings.Contains(s, "cfg-val"), "must include winning value: %s", s)
	assert.True(t, strings.Contains(s, "sources=["), "must list sources: %s", s)
	assert.True(t, strings.Contains(s, "config:cfg-val"), "must show config source: %s", s)
	assert.True(t, strings.Contains(s, "default:dep-val"), "must show default source: %s", s)
}

func TestEffectiveValue_String_EmptySources(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&DefaultSelector[string]{})
	s := ev.String()
	assert.True(t, strings.Contains(s, "strategy=zero"), "empty sources must show zero strategy: %s", s)
	assert.True(t, strings.Contains(s, "sources=[]"), "empty sources must show empty list: %s", s)
}

func TestEffectiveValue_String_SelectorError(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&errorSelector[string]{err: fmt.Errorf("boom")})
	require.NoError(t, ev.SetSource(StrategyDefault, "v"))

	s := ev.String()
	assert.True(t, strings.Contains(s, "<error:"), "error must appear in String(): %s", s)
}

func TestEffectiveValue_String_SourcesListedInPrecedenceOrder(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&DefaultSelector[string]{})
	require.NoError(t, ev.SetSource(StrategyDefault, "dep"))
	require.NoError(t, ev.SetSource(StrategyConfig, "cfg"))
	require.NoError(t, ev.SetSource(StrategyUserInput, "user"))

	s := ev.String()
	// userInput should appear before config, config before default (precedence order).
	userIdx := strings.Index(s, "userInput:")
	cfgIdx := strings.Index(s, "config:")
	defIdx := strings.Index(s, "default:")
	assert.True(t, userIdx < cfgIdx, "userInput must appear before config in sources list: %s", s)
	assert.True(t, cfgIdx < defIdx, "config must appear before default in sources list: %s", s)
}

// ── Non-string types ──────────────────────────────────────────────────────────

func TestEffectiveValue_IntType_Works(t *testing.T) {
	ev, err := NewEffectiveValue[int](&DefaultSelector[int]{})
	require.NoError(t, err)

	require.NoError(t, ev.SetSource(StrategyDefault, 42))
	assert.Equal(t, 42, ev.Get().Val())
	assert.Equal(t, StrategyDefault, ev.Strategy())
}

func TestEffectiveValue_BoolType_Works(t *testing.T) {
	ev, err := NewEffectiveValue[bool](&DefaultSelector[bool]{})
	require.NoError(t, err)

	require.NoError(t, ev.SetSource(StrategyConfig, true))
	assert.True(t, ev.Get().Val())
}

// ── MarshalJSON ───────────────────────────────────────────────────────────────

// unmarshalEV is a helper that unmarshals a MarshalJSON result into a plain map.
func unmarshalEV(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(data, &out))
	return out
}

func TestEffectiveValue_MarshalJSON_ProducesStructuredJSONObject(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&DefaultSelector[string]{})
	require.NoError(t, ev.SetSource(StrategyDefault, "dep-val"))
	require.NoError(t, ev.SetSource(StrategyConfig, "cfg-val"))

	data, err := json.Marshal(ev)
	require.NoError(t, err)

	out := unmarshalEV(t, data)
	assert.Equal(t, "config", out["strategy"], "strategy field must name the winning strategy")
	assert.Equal(t, "cfg-val", out["value"], "value field must hold the winning value")

	sources, ok := out["sources"].(map[string]any)
	require.True(t, ok, "sources must be a JSON object, got: %T", out["sources"])
	assert.Equal(t, "cfg-val", sources["config"])
	assert.Equal(t, "dep-val", sources["default"])
}

func TestEffectiveValue_MarshalJSON_NotAPlainString(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&DefaultSelector[string]{})
	require.NoError(t, ev.SetSource(StrategyConfig, "ns"))

	data, err := json.Marshal(ev)
	require.NoError(t, err)

	// Must be a JSON object ("{…}"), not a JSON string (""…"") or empty object.
	assert.Equal(t, byte('{'), data[0], "output must be a JSON object, not a string: %s", data)
	assert.NotEqual(t, "{}", string(data), "output must not be the empty object produced by unexported fields")
}

func TestEffectiveValue_MarshalJSON_EmptySources_ProducesZeroStrategy(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&DefaultSelector[string]{})

	data, err := json.Marshal(ev)
	require.NoError(t, err)

	out := unmarshalEV(t, data)
	assert.Equal(t, "zero", out["strategy"])
	assert.Equal(t, "", out["value"])

	sources, ok := out["sources"].(map[string]any)
	require.True(t, ok)
	assert.Empty(t, sources, "no sources registered, sources object must be empty")
}

func TestEffectiveValue_MarshalJSON_SelectorError_SurfacesErrorInValueField(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&errorSelector[string]{err: errors.New("boom")})
	require.NoError(t, ev.SetSource(StrategyDefault, "v"))

	data, err := json.Marshal(ev)
	require.NoError(t, err) // MarshalJSON itself must not error

	out := unmarshalEV(t, data)
	assert.Equal(t, "zero", out["strategy"])
	valStr, _ := out["value"].(string)
	assert.True(t, strings.Contains(valStr, "<error:"), "error must be in value field: %s", valStr)
}

func TestEffectiveValue_MarshalJSON_AllSourcesPresent(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&DefaultSelector[string]{})
	require.NoError(t, ev.SetSource(StrategyDefault, "dep"))
	require.NoError(t, ev.SetSource(StrategyConfig, "cfg"))
	require.NoError(t, ev.SetSource(StrategyEnv, "env"))
	require.NoError(t, ev.SetSource(StrategyUserInput, "user"))
	require.NoError(t, ev.SetSource(StrategyState, "state"))
	require.NoError(t, ev.SetSource(StrategyReality, "reality"))

	data, err := json.Marshal(ev)
	require.NoError(t, err)

	out := unmarshalEV(t, data)
	assert.Equal(t, "reality", out["strategy"])
	assert.Equal(t, "reality", out["value"])

	sources, ok := out["sources"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "dep", sources["default"])
	assert.Equal(t, "cfg", sources["config"])
	assert.Equal(t, "env", sources["env"])
	assert.Equal(t, "user", sources["userInput"])
	assert.Equal(t, "state", sources["state"])
	assert.Equal(t, "reality", sources["reality"])
	assert.NotContains(t, sources, "zero", "StrategyZero must never appear in sources")
}

func TestEffectiveValue_MarshalJSON_IntType(t *testing.T) {
	ev, _ := NewEffectiveValue[int](&DefaultSelector[int]{})
	require.NoError(t, ev.SetSource(StrategyConfig, 42))

	data, err := json.Marshal(ev)
	require.NoError(t, err)

	out := unmarshalEV(t, data)
	assert.Equal(t, "config", out["strategy"])
	// JSON numbers unmarshal as float64 by default.
	assert.InDelta(t, float64(42), out["value"], 0)
}

func TestEffectiveValue_MarshalJSON_UsedViaAnyInterface(t *testing.T) {
	ev, _ := NewEffectiveValue[string](&DefaultSelector[string]{})
	require.NoError(t, ev.SetSource(StrategyConfig, "ns"))

	// Simulate what zerolog's AppendInterface / json.Marshal does for Any().
	data, err := json.Marshal(ev)
	require.NoError(t, err)
	assert.NotEqual(t, "{}", string(data),
		"private-field struct must not produce empty object '{}' — MarshalJSON must be invoked")
	assert.NotEqual(t, "null", string(data))
	assert.Equal(t, byte('{'), data[0], "Any() must embed a JSON object, not a quoted string")
}
