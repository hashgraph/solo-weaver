// SPDX-License-Identifier: Apache-2.0

package featuregate

import (
	"os"
	"strconv"
	"strings"
	"unicode"

	gatekit "github.com/automa-saga/gatekit"
)

// envPrefix is the per-feature opt-in environment-variable prefix. It matches the
// prefix used by the existing command gate (cmd/cli/commands/common,
// SOLO_PROVISIONER_ENABLE_<FEATURE>); the two should converge on this helper when
// #1392 unifies the entry-level command gate with the code-level gate.
const envPrefix = "SOLO_PROVISIONER_ENABLE_"

// OverrideEnvVar returns the opt-in environment variable for a feature, mapping
// the CamelCase feature name to UPPER_SNAKE_CASE. For example
// ConsensusOperatorManaged -> SOLO_PROVISIONER_ENABLE_CONSENSUS_OPERATOR_MANAGED.
// Setting it to a truthy value forces that feature on (a per-feature override);
// a falsy value forces it off.
func OverrideEnvVar(f gatekit.Feature) string {
	return envPrefix + camelToScreamingSnake(string(f))
}

func camelToScreamingSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && unicode.IsUpper(r) {
			prev := rune(s[i-1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToUpper(r))
	}
	return b.String()
}

// EnvOverrides reads the per-feature opt-in env vars for every feature in the
// catalog and returns the ones explicitly set to a parseable bool. An unset or
// unparseable var is omitted (no override).
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
