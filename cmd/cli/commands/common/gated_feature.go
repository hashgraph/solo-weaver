// SPDX-License-Identifier: Apache-2.0

package common

import (
	"github.com/hashgraph/solo-weaver/internal/ui/prompt"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

// gatedFeature describes a block-node "gated network feature" — the host
// firewall and traffic shaping are the two instances. Each is toggled by a
// single opt-in bool flag; the feature's other ("content") flags are only
// meaningful once the gate is on. The descriptor supplies the per-feature
// specifics as data so resolveFeatureGate can drive the identical
// force-fetch → effective-bool → confirm-prompt → mismatch-guard skeleton for
// both (issue #947).
type gatedFeature struct {
	// GateFlag is the opt-in bool flag that turns the feature on/off
	// (e.g. FlagNameFirewallEnabled, FlagNameTrafficShapingEnabled).
	GateFlag string
	// Noun names the feature in prompts/errors, phrased so it reads correctly
	// in both "<noun> is not enabled" and "configure <noun>"
	// (e.g. "the host firewall", "traffic shaping").
	Noun string
	// PromptTitle / PromptDesc drive the interactive enable/disable confirm.
	PromptTitle string
	PromptDesc  string
	// ContentFlags are the feature-only flags that do nothing without the gate.
	// Supplying any of them non-interactively while the gate is off is a hard
	// error (they would otherwise be silently dropped — there is no confirm
	// prompt to catch the mismatch once the opt-in default resolves to false).
	ContentFlags []string
}

// resolveFeatureGate resolves the enable/disable decision for a gated network
// feature. Precedence: CLI gate flag > interactive confirm > seedEnabled
// default. It also enforces the "content flag without the gate" guard for
// non-interactive callers. Shared by ResolveHostFirewallConfig and
// ResolveTrafficShapingConfig so the two features decide "on or off?" the same
// way; each caller then resolves its own content once the gate is on.
//
// seedEnabled is the default the choice falls back to when neither the flag nor
// a prompt decides it: `install` passes false (opt-in), while `reconfigure`
// passes the block node's persisted install/reconfigure decision so a no-flag /
// default-accept run keeps whatever was last chosen.
//
// It requires the feature's gate flag to have been registered on cmd.
func resolveFeatureGate(cmd *cobra.Command, args []string, f gatedFeature, seedEnabled bool) (bool, error) {
	force, err := FlagForce().Value(cmd, args)
	if err != nil {
		return false, errorx.IllegalArgument.Wrap(err, "failed to get %s flag", FlagForce().Name)
	}

	// Seeded from the caller-supplied default, overridden by the flag when set.
	enabled := effectiveBool(cmd, f.GateFlag, seedEnabled)

	// Prompt for the enable/disable choice only when it wasn't already decided
	// on the CLI. Declining here skips the feature's content prompts entirely —
	// there's nothing left to ask once the feature itself is turned off.
	if prompt.ShouldPrompt(force) && !cmd.Flags().Changed(f.GateFlag) {
		confirmed, err := prompt.RunConfirm(f.PromptTitle, f.PromptDesc, enabled)
		if err != nil {
			return false, err
		}
		enabled = confirmed
	}

	// Non-interactive callers that supply the feature's content flags without
	// also passing its gate flag would otherwise have those flags silently
	// ignored: with the opt-in default there's no confirm prompt to catch the
	// mismatch, and the caller just skips configuring any of it.
	if !enabled && !prompt.ShouldPrompt(force) {
		for _, name := range f.ContentFlags {
			if cmd.Flags().Changed(name) {
				return false, errorx.IllegalArgument.New(
					"--%s was supplied but %s is not enabled (--%s defaults to false)", name, f.Noun, f.GateFlag).
					WithProperty(models.ErrPropertyResolution, []string{
						"Pass --" + f.GateFlag + "=true to actually apply these settings",
						"Or drop --" + name + " if you did not intend to configure " + f.Noun,
					})
			}
		}
	}

	return enabled, nil
}

// effectiveBool returns the effective value for a bool flag: the flag when
// explicitly set, else cfgVal (the caller-supplied "unset" default). Used by
// resolveFeatureGate to resolve each feature's enable/disable gate.
func effectiveBool(cmd *cobra.Command, name string, cfgVal bool) bool {
	if cmd.Flags().Changed(name) {
		v, _ := cmd.Flags().GetBool(name)
		return v
	}
	return cfgVal
}
