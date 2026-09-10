// SPDX-License-Identifier: Apache-2.0

package common

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

// This is the *experimental-command* feature gate. It is unrelated to the
// block-node gatedFeature in gated_feature.go, which resolves optional
// install-time features (host firewall, traffic shaping) from their own opt-in
// flags. This gate hides a not-production-ready command (a group or a single
// subcommand) and refuses to run it unless the operator explicitly opts in.

const (
	// experimentalFlagName is the hidden opt-in flag installed on a gated command.
	experimentalFlagName = "experimental"
	// experimentalEnvPrefix is the env-var opt-in prefix; the full var is
	// SOLO_PROVISIONER_ENABLE_<FEATURE> (see ExperimentalEnvVar).
	experimentalEnvPrefix = "SOLO_PROVISIONER_ENABLE_"
)

// ExperimentalEnvVar returns the opt-in environment variable for a feature name,
// e.g. "consensus" -> "SOLO_PROVISIONER_ENABLE_CONSENSUS". Non-alphanumeric
// separators are normalised to underscores so multi-word features stay readable.
func ExperimentalEnvVar(feature string) string {
	up := strings.ToUpper(feature)
	up = strings.NewReplacer("-", "_", " ", "_", ".", "_").Replace(up)
	return experimentalEnvPrefix + up
}

// GateExperimental marks cmd as an experimental feature gate: hidden from --help
// and refused at run time unless the operator opts in via the (hidden)
// --experimental flag or the SOLO_PROVISIONER_ENABLE_<FEATURE> env var. It works
// for both models:
//
//   - a command group — Hidden and the PersistentPreRunE propagate to every
//     subcommand, so the whole tree is gated with one call; and
//   - a single leaf subcommand — only that command is hidden and gated, leaving
//     its (production) siblings untouched.
//
// Cobra runs only the nearest PersistentPreRunE, so the installed hook chains the
// command's existing PersistentPreRunE (or the root pre-run when none is set) to
// preserve global checks + startup migrations — callers cannot accidentally skip
// them. --help is handled before PreRun, so gated commands stay self-documenting.
func GateExperimental(cmd *cobra.Command, feature string) {
	cmd.Hidden = true
	envVar := ExperimentalEnvVar(feature)

	// Register the hidden opt-in once. When gating a subcommand under a group that
	// is already gated, the flag is inherited and must not be redeclared.
	if cmd.PersistentFlags().Lookup(experimentalFlagName) == nil {
		cmd.PersistentFlags().Bool(experimentalFlagName, false,
			"Enable this experimental feature (not supported in production)")
		_ = cmd.PersistentFlags().MarkHidden(experimentalFlagName)
	}

	prev := cmd.PersistentPreRunE
	cmd.PersistentPreRunE = func(c *cobra.Command, args []string) error {
		// Preserve whatever pre-run would otherwise run for this command: its own
		// prior hook if it had one, else the root pre-run (global checks + startup
		// migrations), which cobra would skip now that this hook is the closest.
		if prev != nil {
			if err := prev(c, args); err != nil {
				return err
			}
		} else if err := RunPersistentPreRun(c, args); err != nil {
			return err
		}
		return requireExperimentalEnabled(c, feature, envVar)
	}
}

// requireExperimentalEnabled returns nil when the feature is opted in via the
// --experimental flag or a truthy env var, otherwise a decorated refusal.
func requireExperimentalEnabled(cmd *cobra.Command, feature, envVar string) error {
	if enabled, err := cmd.Flags().GetBool(experimentalFlagName); err == nil && enabled {
		return nil
	}
	if v, ok := os.LookupEnv(envVar); ok {
		if enabled, err := strconv.ParseBool(v); err == nil && enabled {
			return nil
		}
	}
	return errx.Decorate(
		errorx.RejectedOperation.New("%s is an experimental feature and not supported in production", feature),
		reasons.PreconditionNotMet,
		"This feature is under active development and not production-ready.",
		fmt.Sprintf("To use it in a non-production environment, pass --experimental (reliable under sudo) or set %s=1.", envVar))
}
