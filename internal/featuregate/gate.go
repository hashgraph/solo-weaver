// SPDX-License-Identifier: Apache-2.0

package featuregate

import gatekit "github.com/automa-saga/gatekit"

// staticSource is a constant VersionSource for the spike. Layer 2 (epic #1275)
// replaces it with a state-backed source whose Current() reads the finalize-
// advanced activation version through the RSL effective-value layer.
type staticSource struct{ v gatekit.Activation }

func (s staticSource) Current() gatekit.Activation { return s.v }

// Options carries the operator-facing opt-ins the CLI resolves and feeds into
// the gate. This is the one place solo maps its own switches onto gatekit:
//
//   - Experimental  <- the --experimental flag  -> gatekit.WithExperimental
//   - Overrides     <- SOLO_PROVISIONER_ENABLE_<FEATURE>=1 -> gatekit.WithOverrides
//
// gatekit owns the decision; solo owns reading the flag/env (gatekit never
// touches cobra). This is the unification epic #1275 calls "one system": the
// same gate serves both the CLI entry gate and code-level behavior gates.
type Options struct {
	// Current is the activation version in force. Zero means CurrentActivationVersion.
	Current gatekit.Activation
	// Experimental opts the gate in to ALL Alpha features gate-wide.
	Experimental bool
	// Overrides forces specific features on/off regardless of version/maturity.
	Overrides map[gatekit.Feature]bool
}

// New builds a FeatureGate over the solo catalog with the given opt-ins.
func New(o Options) (*gatekit.FeatureGate, error) {
	current := o.Current
	if current == 0 {
		current = CurrentActivationVersion
	}

	var opts []gatekit.Option
	if o.Experimental {
		opts = append(opts, gatekit.WithExperimental(true))
	}
	if len(o.Overrides) > 0 {
		opts = append(opts, gatekit.WithOverrides(o.Overrides))
	}

	return gatekit.New(Catalog(), staticSource{v: current}, opts...)
}

// NewDefault builds a gate at CurrentActivationVersion with no opt-in — the
// production default (GA/Beta features on, Alpha features off).
func NewDefault() (*gatekit.FeatureGate, error) {
	return New(Options{})
}
