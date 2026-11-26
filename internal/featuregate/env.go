// SPDX-License-Identifier: Apache-2.0

package featuregate

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/automa-saga/errx"
	gatekit "github.com/automa-saga/gatekit"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
)

// envPrefix is the per-feature opt-in environment-variable prefix. Every
// SOLO_PROVISIONER_ENABLE_* variable names a feature; the experimental command
// gate was converged into the registry (#1392), so there is no separate
// command-gate variable under this prefix.
const envPrefix = "SOLO_PROVISIONER_ENABLE_"

// deprecatedEnvRenames maps opt-in variables from older docs to their current names,
// so a stale one gives an actionable error instead of a bare "unknown variable".
var deprecatedEnvRenames = map[string]string{
	"SOLO_PROVISIONER_ENABLE_CONSENSUS":       "SOLO_PROVISIONER_ENABLE_CONSENSUS_NODE_OPERATOR_MANAGED",
	"SOLO_PROVISIONER_ENABLE_CLUSTER_UPGRADE": "SOLO_PROVISIONER_ENABLE_KUBE_CLUSTER_UPGRADE",
}

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
			hint := fmt.Sprintf("Unset %s, then run `solo-provisioner feature-gate list` to see the valid names.", name)
			if current, renamed := deprecatedEnvRenames[name]; renamed {
				hint = fmt.Sprintf("%s was renamed to %s.", name, current)
			}
			return nil, errx.Decorate(
				errorx.IllegalArgument.New("unknown opt-in env var %q matches no feature", name),
				reasons.InvalidArgument,
				"A SOLO_PROVISIONER_ENABLE_* variable does not match any feature.",
				hint)
		}
		b, err := strconv.ParseBool(val)
		if err != nil {
			return nil, errx.Decorate(
				errorx.IllegalFormat.Wrap(err, "opt-in env var %q has non-boolean value %q", name, val),
				reasons.InvalidArgument,
				"A SOLO_PROVISIONER_ENABLE_* variable must be a boolean.",
				fmt.Sprintf("Set %s to 1/true or 0/false, or unset it.", name))
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

// EnvDisabled reports whether the operator set f's opt-in env var to a falsy value — a
// kill switch that wins over --experimental. Used to explain why a feature is refused.
func EnvDisabled(f gatekit.Feature) bool {
	v, ok := os.LookupEnv(OverrideEnvVar(f))
	if !ok {
		return false
	}
	b, err := strconv.ParseBool(v)
	return err == nil && !b
}
