// SPDX-License-Identifier: Apache-2.0

package featuregate

import (
	"context"

	gatekit "github.com/automa-saga/gatekit"
)

// staticSource is a constant VersionSource for the spike. Layer 2 (epic #1275)
// replaces it with a state-backed source whose Current() reads the finalize-
// advanced activation version through the RSL effective-value layer.
type staticSource struct{ v gatekit.Activation }

func (s staticSource) Current() gatekit.Activation { return s.v }

// Options carries the operator-facing opt-ins the CLI resolves and feeds into the
// gate. There are two operator surfaces, both of which respect each feature's
// readiness (see ExperimentalReady):
//
//   - the SOLO_PROVISIONER_ENABLE_<FEATURE>=1 env var (read globally by New); and
//   - the --experimental flag, which the command layer scopes to THAT command's
//     gated feature and passes in here as Overrides.
//
// gatekit owns the decision; solo owns reading the flag/env (gatekit never touches
// cobra). The gated command and its business code resolve the feature identically —
// the "one system" epic #1275 calls for.
type Options struct {
	// Current is the activation version in force. Zero means CurrentActivationVersion.
	Current gatekit.Activation
	// Overrides forces specific features on/off regardless of version/maturity. It is
	// the raw, readiness-bypassing seam for programmatic/test forcing and for the
	// command layer's already-readiness-checked --experimental opt-in; it is applied
	// last, so it wins over an env opt-in per feature.
	Overrides map[gatekit.Feature]bool
}

// ExperimentalReady reports whether an operator opt-in (--experimental or the
// SOLO_PROVISIONER_ENABLE_<FEATURE> env var) may turn f on. It mirrors gatekit's
// gate-wide alpha opt-in (WithExperimental) resolved for f alone at the current
// activation version:
//
//   - Alpha, Default:false      -> false (dormant: unfinished; no opt-in can enable it)
//   - Alpha, Default:true       -> true  (ready to dogfood)
//   - Beta/GA at/after version  -> true
//   - unknown / not yet active  -> false
//
// This is the readiness gate: an unfinished feature is marked
// Alpha,Default:false and stays off through every operator surface until its spec is
// changed (flipped to Default:true, or graduated). The raw Options.Overrides seam
// still bypasses it for tests and programmatic forcing.
func ExperimentalReady(f gatekit.Feature) (bool, error) {
	return experimentalReadyIn(Catalog(), f, CurrentActivationVersion)
}

func experimentalReadyAt(f gatekit.Feature, v gatekit.Activation) (bool, error) {
	return experimentalReadyIn(Catalog(), f, v)
}

// experimentalReadyIn is the registry-parameterized core of ExperimentalReady (so the
// readiness rule can be unit-tested against both Default values without changing the
// live catalog).
func experimentalReadyIn(reg gatekit.Registry, f gatekit.Feature, v gatekit.Activation) (bool, error) {
	g, err := gatekit.New(reg, staticSource{v: v}, gatekit.WithExperimental(true))
	if err != nil {
		return false, err
	}
	return g.Enabled(f), nil
}

// New builds a FeatureGate over the solo catalog with the given opt-ins.
func New(o Options) (*gatekit.FeatureGate, error) {
	current := o.Current
	if current == 0 {
		current = CurrentActivationVersion
	}

	// Resolve the SOLO_PROVISIONER_ENABLE_<FEATURE> env opt-ins ourselves, so each one
	// respects the feature's readiness instead of forcing on unconditionally.
	// Validation stays strict: an unknown or malformed <prefix>* variable fails New
	// (and stops the binary).
	envReq, err := envFeatureRequests()
	if err != nil {
		return nil, err
	}
	overrides := map[gatekit.Feature]bool{}

	// 1. Env on-requests, readiness-gated: a not-ready feature stays dormant.
	for f, on := range envReq {
		if !on {
			continue
		}
		ready, err := experimentalReadyAt(f, current)
		if err != nil {
			return nil, err
		}
		if ready {
			overrides[f] = true
		}
	}

	// 2. Explicit Options.Overrides: the raw seam — the command layer's --experimental
	// opt-in (already readiness-checked) and programmatic/test forcing.
	for f, v := range o.Overrides {
		overrides[f] = v
	}

	// 3. Env off-requests last, so =0 is a true kill switch: it wins over an on-request
	// AND over --experimental — a feature an operator disabled cannot be resurrected.
	for f, on := range envReq {
		if !on {
			overrides[f] = false
		}
	}

	// WithSupportWindow pins W (the N/N+1 window) to the one value the registry lints
	// also use, so the gate enforces the same window the lints check.
	opts := []gatekit.Option{gatekit.WithSupportWindow(SupportWindow)}
	if len(overrides) > 0 {
		opts = append(opts, gatekit.WithOverrides(overrides))
	}

	return gatekit.New(Catalog(), staticSource{v: current}, opts...)
}

// NewDefault builds a gate at CurrentActivationVersion with no opt-in — the
// production default (GA/Beta features on, Alpha features off, env overrides applied).
func NewDefault() (*gatekit.FeatureGate, error) {
	return New(Options{})
}

type overridesCtxKey struct{}

// WithOverrides returns ctx with the given per-feature opt-in overrides merged into any
// already present, so stacked or nested command gates accumulate rather than replace
// each other. A nil parent defaults to context.Background.
func WithOverrides(ctx context.Context, overrides map[gatekit.Feature]bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(overrides) == 0 {
		return ctx
	}
	merged := map[gatekit.Feature]bool{}
	for f, v := range overridesFromContext(ctx) {
		merged[f] = v
	}
	for f, v := range overrides {
		merged[f] = v
	}
	return context.WithValue(ctx, overridesCtxKey{}, merged)
}

func overridesFromContext(ctx context.Context) map[gatekit.Feature]bool {
	if ctx == nil {
		return nil
	}
	m, _ := ctx.Value(overridesCtxKey{}).(map[gatekit.Feature]bool)
	return m
}

// Gate returns the invocation gate, built from the opt-in overrides accumulated on ctx
// (plus the env opt-ins New always applies). Business code calls this to honour the same
// opt-ins the command gates resolved; it never builds a bespoke gate of its own.
func Gate(ctx context.Context) (*gatekit.FeatureGate, error) {
	return New(Options{Overrides: overridesFromContext(ctx)})
}
