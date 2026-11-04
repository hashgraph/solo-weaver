// SPDX-License-Identifier: Apache-2.0

package common

import (
	"fmt"
	"strings"

	"github.com/automa-saga/errx"
	gatekit "github.com/automa-saga/gatekit"
	"github.com/hashgraph/solo-weaver/internal/featuregate"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

// experimentalFlagName is the hidden gate-wide alpha opt-in flag installed on a
// gated command; it routes to gatekit's WithExperimental.
const experimentalFlagName = "experimental"

// GateFeature ties a command's availability to a gatekit feature's maturity (see
// internal/featuregate). It is the entry-level half of the feature-gate system
// (#1392): a command whose feature does not resolve enabled at the current
// activation version is labelled with its maturity in --help and refuses to run
// unless the operator opts in — gate-wide with the hidden --experimental flag, or
// per feature with the feature's SOLO_PROVISIONER_ENABLE_<FEATURE> override. It
// replaces the bespoke GateExperimental and routes every opt-in through gatekit.
//
// It works for both models:
//
//   - a command group — the PersistentPreRunE and the --experimental flag
//     propagate to every subcommand, so the whole tree is gated with one call; and
//   - a single leaf subcommand — only that command is gated.
//
// Visibility is maturity-driven: an ALPHA command is hidden from --help (not yet
// for end users) but still runs when opted in; a BETA command stays visible with
// a "[beta]" label. Either way it is refused until opted in. A hidden alpha
// command is still discoverable via `solo-provisioner feature list`, which shows
// the feature and its exact opt-in env var.
func GateFeature(cmd *cobra.Command, f gatekit.Feature) {
	// Label / hide the command by its maturity (static, registry-derived).
	switch featuregate.MaturityOf(f) {
	case gatekit.Alpha:
		cmd.Short = strings.TrimSpace(cmd.Short) + " [alpha]"
		cmd.Hidden = true
	case gatekit.Beta:
		cmd.Short = strings.TrimSpace(cmd.Short) + " [beta]"
	}

	// Register the hidden gate-wide opt-in once (inherited by subcommands).
	if cmd.PersistentFlags().Lookup(experimentalFlagName) == nil {
		cmd.PersistentFlags().Bool(experimentalFlagName, false,
			"Enable experimental (alpha) features (not supported in production)")
		_ = cmd.PersistentFlags().MarkHidden(experimentalFlagName)
	}

	prev := cmd.PersistentPreRunE
	cmd.PersistentPreRunE = func(c *cobra.Command, args []string) error {
		// Preserve whatever pre-run would otherwise run: a prior hook if this
		// command had one, else the root pre-run (global checks + startup
		// migrations), which cobra skips now that this hook is the closest.
		if prev != nil {
			if err := prev(c, args); err != nil {
				return err
			}
		} else if err := RunPersistentPreRun(c, args); err != nil {
			return err
		}
		return requireFeatureEnabled(c, f)
	}
}

// requireFeatureEnabled returns nil when the operator may run an f-gated command,
// otherwise a decorated refusal. Command availability is maturity-based: GA/Beta
// commands always run; an Alpha command runs only when opted in (--experimental
// or its override env var). The gate is evaluated at the feature's own activation
// version, so a not-yet-active Alpha command is still enable-able by opting in —
// the activation-version axis governs behaviour rollout, not command availability.
func requireFeatureEnabled(cmd *cobra.Command, f gatekit.Feature) error {
	if featuregate.MaturityOf(f) != gatekit.Alpha {
		return nil
	}
	experimental, _ := cmd.Flags().GetBool(experimentalFlagName)
	gate, err := featuregate.New(featuregate.Options{Experimental: experimental})
	if err != nil {
		return err
	}
	if gate.EnabledAt(f, featuregate.ActiveFromOf(f)) {
		return nil
	}
	return errx.Decorate(
		errorx.RejectedOperation.New("%s is an experimental feature and not supported in production", f),
		reasons.PreconditionNotMet,
		"This feature is under active development and not production-ready.",
		fmt.Sprintf("To use it in a non-production environment, pass --experimental (reliable under sudo) or set %s=1.",
			featuregate.OverrideEnvVar(f)))
}
