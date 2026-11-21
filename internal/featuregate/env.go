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

// envFeatureRequests scans the environment for SOLO_PROVISIONER_ENABLE_* opt-ins and
// returns the on/off state each one requests, keyed by feature. It fails loudly on a
// typo'd opt-in — a variable mapping to no feature, or a non-bool value, errors
// (stopping the binary) rather than being silently ignored. It reports the request
// as-is; New decides whether to honour an on-request (only for a ready feature — see
// ExperimentalReady).
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

// OverrideEnvVar returns the per-feature opt-in environment variable for f (the
// CamelCase -> UPPER_SNAKE_CASE convention is gatekit's, so solo and the library stay
// in lockstep). For example ConsensusNodeOperatorManaged ->
// SOLO_PROVISIONER_ENABLE_CONSENSUS_NODE_OPERATOR_MANAGED. A truthy value requests the
// feature on (honoured only when it is ready); a falsy value forces it off.
func OverrideEnvVar(f gatekit.Feature) string {
	return gatekit.EnvVarName(f, envPrefix)
}
