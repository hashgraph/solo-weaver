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
	for f, on := range envReq {
		if !on {
			overrides[f] = false // an off-request is a kill switch — always honoured
			continue
		}
		ready, err := experimentalReadyAt(f, current)
		if err != nil {
			return nil, err
		}
		if ready {
			overrides[f] = true
		}
		// A not-ready feature stays dormant: the on-request is dropped.
	}

	// Explicit Options.Overrides win over env (applied last — last-writer). This is the
	// raw seam: the command layer's --experimental opt-in (already readiness-checked),
	// and programmatic/test forcing.
	for f, v := range o.Overrides {
		overrides[f] = v
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

type gateCtxKey struct{}

// NewContext returns ctx carrying g, the one gate resolved for this invocation
// (env overrides plus any --experimental opt-in). Command gating installs it so that
// business code later resolves features against the SAME effective config — opting a
// command in via --experimental therefore turns the feature on for its business logic
// too. A nil parent defaults to context.Background.
func NewContext(ctx context.Context, g *gatekit.FeatureGate) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, gateCtxKey{}, g)
}

// FromContext returns the gate installed by NewContext, or nil when none is set
// (including a nil ctx).
func FromContext(ctx context.Context) *gatekit.FeatureGate {
	if ctx == nil {
		return nil
	}
	g, _ := ctx.Value(gateCtxKey{}).(*gatekit.FeatureGate)
	return g
}

// Gate returns the invocation's gate from ctx, or a default gate (env overrides, no
// --experimental) when none was installed. Business code calls this to honour the same
// opt-ins the command gate resolved; it never builds a bespoke gate of its own.
func Gate(ctx context.Context) (*gatekit.FeatureGate, error) {
	if g := FromContext(ctx); g != nil {
		return g, nil
	}
	return NewDefault()
}
