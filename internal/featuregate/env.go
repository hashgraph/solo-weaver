// SPDX-License-Identifier: Apache-2.0

package featuregate

import (
	"os"
	"strconv"

	gatekit "github.com/automa-saga/gatekit"
)

// envPrefix is the per-feature opt-in environment-variable prefix.
//
// NOTE: this prefix is shared with the entry-level command gate
// (cmd/cli/commands/common/experimental_gate.go, e.g.
// SOLO_PROVISIONER_ENABLE_CONSENSUS). Because of that overlap we do NOT use
// gatekit's strict WithEnvOverrides here yet: it errors on any <prefix>* var that
// is not a registered feature, which would reject the command gate's vars. The
// known-features-only scan below (EnvOverrides) ignores unrelated vars. Adopting
// WithEnvOverrides — and its fail-fast on a mistyped opt-in — lands with #1392,
// which converges the two gates onto one registry so every <prefix>* var is a
// feature and strict validation is safe.
const envPrefix = "SOLO_PROVISIONER_ENABLE_"

// OverrideEnvVar returns the per-feature opt-in environment variable for f. The
// name convention (CamelCase -> UPPER_SNAKE_CASE) is owned by gatekit, so solo
// and the library stay in lockstep. For example ConsensusNodeOperatorManaged ->
// SOLO_PROVISIONER_ENABLE_CONSENSUS_NODE_OPERATOR_MANAGED. A truthy value forces
// the feature on (a per-feature override); a falsy value forces it off.
func OverrideEnvVar(f gatekit.Feature) string {
	return gatekit.EnvVarName(f, envPrefix)
}

// EnvOverrides reads the per-feature opt-in env vars for every feature in the
// catalog and returns the ones explicitly set to a parseable bool. An unset or
// unparseable var is omitted (no override). Unknown <prefix>* vars are ignored
// (see the envPrefix note) — unlike gatekit's strict WithEnvOverrides.
func EnvOverrides() map[gatekit.Feature]bool {
	out := map[gatekit.Feature]bool{}
	for f := range Catalog() {
		if v, ok := os.LookupEnv(OverrideEnvVar(f)); ok {
			if b, err := strconv.ParseBool(v); err == nil {
				out[f] = b
			}
		}
	}
	return out
}
