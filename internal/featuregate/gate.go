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

// Options carries the operator-facing opt-ins the CLI resolves and feeds into
// the gate. The opt-in is always a per-feature override (version-independent), from
// one of two surfaces:
//
//   - the SOLO_PROVISIONER_ENABLE_<FEATURE>=1 env var (applied globally via
//     gatekit.WithEnvOverrides); and
//   - the --experimental flag, which the command layer resolves to an override of
//     THAT command's gated feature(s) and passes in here as Overrides.
//
// gatekit owns the decision; solo owns reading the flag/env (gatekit never touches
// cobra). Because the opt-in is an override, the gated command and its business code
// resolve the feature identically — the "one system" epic #1275 calls for.
type Options struct {
	// Current is the activation version in force. Zero means CurrentActivationVersion.
	Current gatekit.Activation
	// Overrides forces specific features on/off regardless of version/maturity.
	Overrides map[gatekit.Feature]bool
}

// New builds a FeatureGate over the solo catalog with the given opt-ins.
func New(o Options) (*gatekit.FeatureGate, error) {
	current := o.Current
	if current == 0 {
		current = CurrentActivationVersion
	}

	// Per-feature opt-ins from SOLO_PROVISIONER_ENABLE_<FEATURE> env vars. Strict:
	// an unknown or malformed <prefix>* variable fails New (and stops the binary).
	// Safe now that every SOLO_PROVISIONER_ENABLE_* var maps to a feature — the
	// experimental command gate was converged into the registry (#1392).
	opts := []gatekit.Option{gatekit.WithEnvOverrides(envPrefix)}

	// Explicit overrides win over env (applied last — gatekit is last-writer). This is
	// where the --experimental opt-in lands, as a per-feature override.
	if len(o.Overrides) > 0 {
		opts = append(opts, gatekit.WithOverrides(o.Overrides))
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
