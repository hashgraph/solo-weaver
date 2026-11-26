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

// Seams so tests can exercise BETA/GA/dormant paths the live catalog doesn't have.
var (
	maturityOf        = featuregate.MaturityOf
	experimentalReady = featuregate.ExperimentalReady
)

// GateFeature gates a command by its feature's maturity: an ALPHA command is hidden
// from --help and refuses to run unless the operator opts in (--experimental or
// SOLO_PROVISIONER_ENABLE_<FEATURE>) and the feature is ready; a BETA command is
// visible, labelled "[beta]", and runs; a GA command runs. On a command group it gates
// the whole subtree; on a leaf, just that command. Hidden alpha commands stay
// discoverable via `solo-provisioner feature-gate list`.
func GateFeature(cmd *cobra.Command, f gatekit.Feature) {
	// Label / hide the command by its maturity (static, registry-derived).
	switch maturityOf(f) {
	case gatekit.Alpha:
		cmd.Short = withLabel(cmd.Short, "[alpha]")
		cmd.Hidden = true
	case gatekit.Beta:
		cmd.Short = withLabel(cmd.Short, "[beta]")
	case "":
		// Uncatalogued feature (a wiring bug): hide the command; requireFeatureEnabled
		// refuses it at run time so it never ships open.
		cmd.Hidden = true
	}

	installExperimentalFlag(cmd)
	chainPersistentPreRunE(cmd, func(c *cobra.Command, _ []string) error {
		return requireFeatureEnabled(c, f)
	})
}

// withLabel appends label to s once, so re-gating a command doesn't double it.
func withLabel(s, label string) string {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, label) {
		return s
	}
	return s + " " + label
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
		// Wiring bug: the flag isn't registered, so there is nothing to gate. Fail
		// closed — refuse the command rather than silently leaving the flag ungated.
		chainPersistentPreRunE(cmd, func(c *cobra.Command, _ []string) error {
			return errx.Decorate(
				errorx.IllegalState.New("feature-gate wiring: flag %q is not registered on %q", flagName, cmd.Name()),
				reasons.Internal,
				"This is a feature-gate wiring bug.",
				"Register the flag before gating it with GateFeatureFlag.")
		})
		return
	}

	installExperimentalFlag(cmd)

	switch maturityOf(f) {
	case gatekit.Alpha:
		flag.Hidden = true
		flag.Usage = withLabel(flag.Usage, "[alpha]")
	case gatekit.Beta:
		flag.Usage = withLabel(flag.Usage, "[beta]")
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
		// be checked here to cover subcommands.
		chainPersistentPreRunE(cmd, enforce)
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

// chainPersistentPreRunE appends next after the pre-run cobra would have run for the
// command. cobra runs only the CLOSEST PersistentPreRunE, so when a child installs its
// own hook it would otherwise shadow (and skip) an ancestor group's gate — so the chain
// re-invokes the nearest ancestor's hook rather than jumping straight to the root.
func chainPersistentPreRunE(cmd *cobra.Command, next func(*cobra.Command, []string) error) {
	prev := cmd.PersistentPreRunE
	cmd.PersistentPreRunE = func(c *cobra.Command, args []string) error {
		if err := runInheritedPreRun(cmd, prev, c, args); err != nil {
			return err
		}
		return next(c, args)
	}
}

// runInheritedPreRun runs the PersistentPreRun(E) cobra would have run for c had our hook
// not shadowed it: cmd's prior hook, else the nearest ancestor's, else the root pre-run.
func runInheritedPreRun(cmd *cobra.Command, prev func(*cobra.Command, []string) error, c *cobra.Command, args []string) error {
	if prev != nil {
		return prev(c, args)
	}
	for p := cmd.Parent(); p != nil; p = p.Parent() {
		if p.PersistentPreRunE != nil {
			return p.PersistentPreRunE(c, args)
		}
		if p.PersistentPreRun != nil {
			p.PersistentPreRun(c, args)
			return nil
		}
	}
	return RunPersistentPreRun(c, args)
}

// GateBehavior wires the --experimental opt-in onto a GA command that reads one or more
// code-path gates (gate.Enabled) but is itself always available — it is never hidden and
// never refused. Passing --experimental turns the declared ready features on for the
// invocation (scoped to these features, readiness-respecting) via the shared context
// gate, so business code reading featuregate.Gate(ctx) sees them enabled. Being a flag,
// it survives sudo — unlike the SOLO_PROVISIONER_ENABLE_<FEATURE> env var (#1487).
func GateBehavior(cmd *cobra.Command, features ...gatekit.Feature) {
	installExperimentalFlag(cmd)
	chainPersistentPreRunE(cmd, func(c *cobra.Command, _ []string) error {
		return applyBehaviorGate(c, features)
	})
}

// applyBehaviorGate accumulates the declared ready features onto the command context's
// opt-in overrides when --experimental is set, so business code (featuregate.Gate(ctx))
// and any stacked gate see them enabled.
func applyBehaviorGate(cmd *cobra.Command, features []gatekit.Feature) error {
	if len(features) == 0 {
		return errx.Decorate(
			errorx.IllegalState.New("GateBehavior on %q declared no features", cmd.Name()),
			reasons.Internal,
			"This is a feature-gate wiring bug.",
			"Pass the code-path features the command reads.")
	}
	// Validate the wiring on every run (fail closed), independent of --experimental.
	for _, f := range features {
		if maturityOf(f) == "" {
			return errx.Decorate(
				errorx.IllegalState.New("feature %q is gated but not registered in the catalog", f),
				reasons.Internal,
				"This is a feature-gate wiring bug.",
				"Add the feature to internal/featuregate/registry.go (and features.go).")
		}
	}
	if experimental, _ := cmd.Flags().GetBool(experimentalFlagName); experimental {
		overrides := make(map[gatekit.Feature]bool, len(features))
		for _, f := range features {
			ready, err := experimentalReady(f)
			if err != nil {
				return err
			}
			if ready {
				overrides[f] = true // a dormant feature stays off
			}
		}
		cmd.SetContext(featuregate.WithOverrides(cmd.Context(), overrides))
	}
	return nil
}

// requireFeatureEnabled returns nil when the operator may run a command gated by
// feature f, else a decorated refusal. GA/BETA commands always pass; an ALPHA command
// passes only when opted in (--experimental or the env var) AND ready. It accumulates the
// opt-in on the command context so business code (featuregate.Gate(ctx)) resolves the
// feature the same way.
func requireFeatureEnabled(cmd *cobra.Command, f gatekit.Feature) error {
	maturity := maturityOf(f)
	if maturity == "" {
		// Wiring bug: a command is gated on a feature that is not in the catalog. Fail
		// closed so it never runs open (the command is also hidden by GateFeature).
		return errx.Decorate(
			errorx.IllegalState.New("feature %q is gated but not registered in the catalog", f),
			reasons.Internal,
			"This is a feature-gate wiring bug.",
			"Add the feature to internal/featuregate/registry.go (and features.go).")
	}
	if maturity != gatekit.Alpha {
		return nil
	}

	// --experimental only enables a ready alpha; for a dormant one it is dropped.
	ready, err := experimentalReady(f)
	if err != nil {
		return err
	}
	if experimental, _ := cmd.Flags().GetBool(experimentalFlagName); experimental && ready {
		// Accumulate on the context (not replace), so business code and any stacked gate
		// see the same opt-ins.
		cmd.SetContext(featuregate.WithOverrides(cmd.Context(), map[gatekit.Feature]bool{f: true}))
	}
	gate, err := featuregate.Gate(cmd.Context())
	if err != nil {
		return err
	}

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
	if featuregate.EnvDisabled(f) {
		// An env off-request (=0) is a kill switch that wins over --experimental; name it
		// as the blocker instead of telling the operator to set it to 1.
		return errx.Decorate(
			errorx.RejectedOperation.New("%s is disabled by %s=0", f, featuregate.OverrideEnvVar(f)),
			reasons.PreconditionNotMet,
			"This feature is explicitly disabled by an environment variable.",
			fmt.Sprintf("Unset %s to allow it.", featuregate.OverrideEnvVar(f)))
	}
	return errx.Decorate(
		errorx.RejectedOperation.New("%s is an experimental feature and not supported in production", f),
		reasons.PreconditionNotMet,
		"This feature is under active development and not production-ready.",
		fmt.Sprintf("To use it in a non-production environment, pass --experimental (reliable under sudo) or set %s=1.",
			featuregate.OverrideEnvVar(f)))
}
