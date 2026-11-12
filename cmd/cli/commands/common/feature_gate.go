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

	installExperimentalFlag(cmd)

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

// GateFeatureFlag ties a single flag's visibility and availability to a feature's
// maturity: an ALPHA flag is hidden from --help and errors if the operator sets
// it without opting in; a BETA flag is labelled "[beta]". The flag must already
// be registered on cmd (local or persistent). Opt-in is the same as GateFeature:
// --experimental, or the feature's SOLO_PROVISIONER_ENABLE_<FEATURE> override.
//
// A persistent flag is inherited by cmd's subcommands, so the check is installed on
// PersistentPreRunE (which cobra inherits), not PreRunE (which it does not) — a
// subcommand that uses the inherited flag is gated too. A local flag is gated on
// cmd's own PreRunE.
//
// Use this when a stable (GA) command gains an unproven flag — gate just the
// flag, not the whole command.
func GateFeatureFlag(cmd *cobra.Command, flagName string, f gatekit.Feature) {
	// A persistent flag is inherited by subcommands; a local flag is not. This
	// decides where the gate check must live (see below).
	persistent := cmd.PersistentFlags().Lookup(flagName)
	flag := persistent
	if flag == nil {
		flag = cmd.Flags().Lookup(flagName)
	}
	if flag == nil {
		return
	}

	installExperimentalFlag(cmd)

	switch featuregate.MaturityOf(f) {
	case gatekit.Alpha:
		flag.Hidden = true
		flag.Usage = strings.TrimSpace(flag.Usage) + " [alpha]"
	case gatekit.Beta:
		flag.Usage = strings.TrimSpace(flag.Usage) + " [beta]"
	}

	// enforce refuses the flag unless the operator opted in — but only when the flag
	// was actually set.
	enforce := func(c *cobra.Command, args []string) error {
		if c.Flags().Changed(flagName) {
			return requireFeatureEnabled(c, f)
		}
		return nil
	}

	if persistent != nil {
		// A persistent flag can be set on cmd OR any subcommand that inherits it.
		// cobra does NOT inherit PreRunE to subcommands, so a check installed there
		// would let a subcommand use the flag unchecked. PersistentPreRunE IS
		// inherited (cobra runs the nearest one up the chain), so install the check
		// there, preserving the prior pre-run — a chained hook, or the root pre-run
		// cobra would otherwise skip now that this hook is the closest.
		prev := cmd.PersistentPreRunE
		cmd.PersistentPreRunE = func(c *cobra.Command, args []string) error {
			if prev != nil {
				if err := prev(c, args); err != nil {
					return err
				}
			} else if err := RunPersistentPreRun(c, args); err != nil {
				return err
			}
			return enforce(c, args)
		}
		return
	}

	// A local flag is usable only on cmd itself, so PreRunE is sufficient.
	prev := cmd.PreRunE
	cmd.PreRunE = func(c *cobra.Command, args []string) error {
		if prev != nil {
			if err := prev(c, args); err != nil {
				return err
			}
		}
		return enforce(c, args)
	}
}

// installExperimentalFlag registers the hidden gate-wide --experimental opt-in
// once on cmd (inherited by subcommands when persistent).
func installExperimentalFlag(cmd *cobra.Command) {
	if cmd.PersistentFlags().Lookup(experimentalFlagName) == nil {
		cmd.PersistentFlags().Bool(experimentalFlagName, false,
			"Enable experimental (alpha) features (not supported in production)")
		_ = cmd.PersistentFlags().MarkHidden(experimentalFlagName)
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
