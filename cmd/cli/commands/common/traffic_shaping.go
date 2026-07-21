// SPDX-License-Identifier: Apache-2.0

package common

import (
	"github.com/hashgraph/solo-weaver/internal/ui/prompt"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

// FlagNameTrafficShapingEnabled is the CLI flag name for the top-level gate
// over the BN workload network-policy plane (`inet weaver` classification) and
// tc HTB traffic shaping. It is only registered on `block node install` today:
// `reconfigure`/`upgrade` only re-persist an already-created plane and have no
// equivalent toggle — tearing an existing plane down is a separate concern.
const FlagNameTrafficShapingEnabled = "traffic-shaping-enabled"

// RegisterTrafficShapingFlags registers the top-level traffic-shaping gate flag
// on cmd. The value is read back by name in ResolveTrafficShapingConfig.
func RegisterTrafficShapingFlags(cmd *cobra.Command) {
	cmd.Flags().Bool(FlagNameTrafficShapingEnabled, false,
		"Create the BN workload network-policy plane (inet weaver classification) and tc HTB traffic "+
			"shaping, and install the traffic-shaper daemon. Opt-in (default: false) so existing "+
			"non-interactive callers are unaffected; enable explicitly to get all three.")
}

// ResolveTrafficShapingConfig determines whether the traffic-shaping/network-
// policy bundle should be created for this install, prompting for the choice
// when the session is interactive and the flag was not supplied on the CLI.
// Accepting is the caller's signal to run the egress NIC/link-rate prompts
// (ResolveEgressConfig), the NetworkPolicyCreate/NftWeaverPersist/
// TcEgressPersist/TcIngressRecord steps, and to install and provision the
// traffic-shaper daemon automatically at the end of install — daemon
// activation is not a separate question, it follows directly from this one.
// Declining skips all of it: there is nothing for any of them to configure
// once the policy plane itself is off.
//
// It requires RegisterTrafficShapingFlags to have been called on cmd.
func ResolveTrafficShapingConfig(cmd *cobra.Command, args []string, cv *prompt.ChosenValues) (bool, error) {
	force, err := FlagForce().Value(cmd, args)
	if err != nil {
		return false, errorx.IllegalArgument.Wrap(err, "failed to get %s flag", FlagForce().Name)
	}

	// Seeded false (opt-in) so existing non-interactive/scripted installs that
	// don't pass this flag keep today's behavior rather than silently picking
	// up the policy plane, tc shaping, and daemon install.
	enabled := effectiveBool(cmd, FlagNameTrafficShapingEnabled, false)

	// Prompt for the enable/disable choice only when it wasn't already decided
	// on the CLI. Declining here skips the egress prompts below entirely —
	// there's nothing left to ask once the plane itself is turned off.
	if prompt.ShouldPrompt(force) && !cmd.Flags().Changed(FlagNameTrafficShapingEnabled) {
		confirmed, err := prompt.RunConfirm(
			"Enable traffic shaping?",
			"Create the BN workload network-policy plane (inet weaver classification) and tc HTB "+
				"traffic shaping, and install the traffic-shaper daemon. Opt-in, default No — choose Yes "+
				"to get all three.",
			enabled,
		)
		if err != nil {
			return false, err
		}
		enabled = confirmed
	}

	// Non-interactive callers that supply traffic-shaping-only flags (egress
	// NIC/link-rate, --shape overrides, daemon binary source) without also
	// passing --traffic-shaping-enabled=true would otherwise have those flags
	// silently ignored: with the opt-in default there's no confirm prompt to
	// catch the mismatch, and the caller just skips configuring any of it.
	if !enabled && !prompt.ShouldPrompt(force) {
		for _, name := range []string{
			FlagNameEgressInterface, FlagNameLinkRate, FlagNameShape,
			FlagDaemonBin().Name, FlagDaemonVersion().Name,
		} {
			if cmd.Flags().Changed(name) {
				return false, errorx.IllegalArgument.New(
					"--%s was supplied but traffic shaping is not enabled (--traffic-shaping-enabled defaults to false)", name).
					WithProperty(models.ErrPropertyResolution, []string{
						"Pass --traffic-shaping-enabled=true to actually apply these settings",
						"Or drop --" + name + " if you did not intend to configure traffic shaping",
					})
			}
		}
	}

	return enabled, nil
}
