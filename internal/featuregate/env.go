// SPDX-License-Identifier: Apache-2.0

package featuregate

import (
	"os"
	"strconv"
	"strings"

	gatekit "github.com/automa-saga/gatekit"
	"github.com/joomcode/errorx"
)

// envPrefix is the per-feature opt-in environment-variable prefix. Every
// SOLO_PROVISIONER_ENABLE_* variable names a feature; the experimental command
// gate was converged into the registry (#1392), so there is no separate
// command-gate variable under this prefix.
const envPrefix = "SOLO_PROVISIONER_ENABLE_"

// envFeatureRequests scans the process environment for SOLO_PROVISIONER_ENABLE_*
// opt-in variables and returns the on/off state each one requests, keyed by feature.
//
// It owns the strict validation gatekit.WithEnvOverrides used to provide, so a
// typo'd opt-in still fails loudly instead of silently doing nothing: a <prefix>*
// variable that maps to no catalogued feature, or whose value is not a parseable
// bool, returns an error (which stops the binary).
//
// It does NOT apply readiness — it reports what the operator asked for. New decides
// whether an on-request is honoured (Model C: an on-request only takes effect for an
// ExperimentalReady feature, so an Alpha,Default:false feature stays dormant). An
// off-request is always honoured (a kill switch).
func envFeatureRequests() (map[gatekit.Feature]bool, error) {
	// Reverse-map each feature's canonical opt-in variable name for this catalog.
	byVar := map[string]gatekit.Feature{}
	for f := range Catalog() {
		byVar[OverrideEnvVar(f)] = f
	}

	out := map[gatekit.Feature]bool{}
	for _, kv := range os.Environ() {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		name, val := kv[:eq], kv[eq+1:]
		if !strings.HasPrefix(name, envPrefix) {
			continue
		}
		f, ok := byVar[name]
		if !ok {
			return nil, errorx.IllegalArgument.New("featuregate: unknown opt-in env var %q matches no feature", name)
		}
		b, err := strconv.ParseBool(val)
		if err != nil {
			return nil, errorx.IllegalFormat.Wrap(err, "featuregate: opt-in env var %q has non-boolean value %q", name, val)
		}
		out[f] = b
	}
	return out, nil
}

// OverrideEnvVar returns the per-feature opt-in environment variable for f. The
// name convention (CamelCase -> UPPER_SNAKE_CASE) is owned by gatekit, so solo
// and the library stay in lockstep. For example ConsensusNodeOperatorManaged ->
// SOLO_PROVISIONER_ENABLE_CONSENSUS_NODE_OPERATOR_MANAGED. A truthy value forces
// the feature on (a per-feature override); a falsy value forces it off.
func OverrideEnvVar(f gatekit.Feature) string {
	return gatekit.EnvVarName(f, envPrefix)
}
