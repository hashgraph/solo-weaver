// SPDX-License-Identifier: Apache-2.0

package featuregate

import gatekit "github.com/automa-saga/gatekit"

// envPrefix is the per-feature opt-in environment-variable prefix. Every
// SOLO_PROVISIONER_ENABLE_* variable is a feature override — the experimental
// command gate was converged into the registry (#1392), so there is no longer a
// separate command-gate variable under this prefix, and `New` can validate it
// strictly via gatekit.WithEnvOverrides.
const envPrefix = "SOLO_PROVISIONER_ENABLE_"

// OverrideEnvVar returns the per-feature opt-in environment variable for f. The
// name convention (CamelCase -> UPPER_SNAKE_CASE) is owned by gatekit, so solo
// and the library stay in lockstep. For example ConsensusNodeOperatorManaged ->
// SOLO_PROVISIONER_ENABLE_CONSENSUS_NODE_OPERATOR_MANAGED. A truthy value forces
// the feature on (a per-feature override); a falsy value forces it off.
func OverrideEnvVar(f gatekit.Feature) string {
	return gatekit.EnvVarName(f, envPrefix)
}
