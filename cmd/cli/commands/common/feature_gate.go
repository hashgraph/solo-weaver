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

// experimentalFlagName is the hidden, command-scoped alpha opt-in flag. It enables the
// command's feature only if it is ready (see featuregate.ExperimentalReady).
const experimentalFlagName = "experimental"

// GateFeature gates a command by its feature's maturity: an ALPHA command is hidden
// from --help, a BETA command is labelled "[beta]", and both refuse to run unless the
// operator opts in (--experimental or SOLO_PROVISIONER_ENABLE_<FEATURE>) and the
// feature is ready; GA commands always run. On a command group it gates the whole
// subtree; on a leaf, just that command. Hidden alpha commands stay discoverable via
// `solo-provisioner feature list`.
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
		// Chain the prior pre-run (or the root pre-run cobra now skips), then the gate.
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

// GateFeatureFlag gates a single flag (not the whole command) by its feature's
// maturity — use it when a GA command gains an unproven flag. An ALPHA flag is hidden
// and errors if set without opting in; a BETA flag is labelled "[beta]". Opt-in is the
// same as GateFeature. The flag must already be registered on cmd.
func GateFeatureFlag(cmd *cobra.Command, flagName string, f gatekit.Feature) {
	// A persistent flag is inherited by subcommands; a local one is not — this decides
	// where the check must live (see below).
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

	// enforce refuses the flag unless opted in — but only when the flag was set.
	enforce := func(c *cobra.Command, args []string) error {
		if c.Flags().Changed(flagName) {
			return requireFeatureEnabled(c, f)
		}
		return nil
	}

	if persistent != nil {
		// cobra inherits PersistentPreRunE but NOT PreRunE, so an inherited flag must
		// be checked here to cover subcommands. Chain the prior/root pre-run first.
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

// installExperimentalFlag registers the hidden, command-scoped --experimental opt-in
// once on cmd (inherited by subcommands when persistent).
func installExperimentalFlag(cmd *cobra.Command) {
	if cmd.PersistentFlags().Lookup(experimentalFlagName) == nil {
		cmd.PersistentFlags().Bool(experimentalFlagName, false,
			"Enable experimental (alpha) features (not supported in production)")
		_ = cmd.PersistentFlags().MarkHidden(experimentalFlagName)
	}
}

// requireFeatureEnabled returns nil when the operator may run a command gated by
// feature f, else a decorated refusal. GA/BETA commands always pass; an ALPHA command
// passes only when opted in (--experimental or the env var) AND ready. It stores the
// resolved gate on the command context so business code (featuregate.Gate(ctx)) resolves
// the feature the same way.
func requireFeatureEnabled(cmd *cobra.Command, f gatekit.Feature) error {
	if featuregate.MaturityOf(f) != gatekit.Alpha {
		return nil
	}

	// --experimental only enables a ready alpha; for a dormant one it is dropped.
	ready, err := featuregate.ExperimentalReady(f)
	if err != nil {
		return err
	}
	var overrides map[gatekit.Feature]bool
	if experimental, _ := cmd.Flags().GetBool(experimentalFlagName); experimental && ready {
		overrides = map[gatekit.Feature]bool{f: true}
	}
	gate, err := featuregate.New(featuregate.Options{Overrides: overrides})
	if err != nil {
		return err
	}
	// Share this exact gate with the command's business code.
	cmd.SetContext(featuregate.NewContext(cmd.Context(), gate))

	if gate.Enabled(f) {
		return nil
	}
	if !ready {
		// The feature is registered but unfinished: no opt-in can enable it.
		return errx.Decorate(
			errorx.RejectedOperation.New("%s is under active development and cannot be enabled yet", f),
			reasons.PreconditionNotMet,
			"This feature is incomplete and is intentionally not switchable on.",
			"It becomes available once it is marked ready in a future release.")
	}
	return errx.Decorate(
		errorx.RejectedOperation.New("%s is an experimental feature and not supported in production", f),
		reasons.PreconditionNotMet,
		"This feature is under active development and not production-ready.",
		fmt.Sprintf("To use it in a non-production environment, pass --experimental (reliable under sudo) or set %s=1.",
			featuregate.OverrideEnvVar(f)))
}
