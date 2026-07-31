// SPDX-License-Identifier: Apache-2.0

package common

import (
	"github.com/hashgraph/solo-weaver/internal/ui/prompt"
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
// seedEnabled is the default the choice falls back to when neither the flag nor
// an interactive prompt decides it: `install` passes false (opt-in — a fresh
// install without the flag stays unshaped), while `reconfigure` passes the block
// node's persisted traffic-shaping decision (BlockNodeState.TrafficShapingDisabled)
// so a no-flag / default-accept reconfigure keeps the existing decision rather than
// silently tearing an established plane down.
//
// It requires RegisterTrafficShapingFlags to have been called on cmd.
func ResolveTrafficShapingConfig(cmd *cobra.Command, args []string, cv *prompt.ChosenValues, seedEnabled bool) (bool, error) {
	// Traffic shaping and the host firewall are gated identically (flag >
	// confirm-prompt > seed, plus the content-flag-without-gate guard); the only
	// difference is the flag/noun/prompt wording and the content-flag set, which
	// trafficShapingFeature supplies as data.
	return resolveFeatureGate(cmd, args, trafficShapingFeature(), seedEnabled)
}

// trafficShapingFeature describes traffic shaping as a gated network feature for
// resolveFeatureGate: the --traffic-shaping-enabled opt-in gate plus the
// traffic-shaping-only flags (egress NIC/link-rate, --shape overrides, daemon
// binary source) that are meaningless without it.
func trafficShapingFeature() gatedFeature {
	return gatedFeature{
		GateFlag:    FlagNameTrafficShapingEnabled,
		Noun:        "traffic shaping",
		PromptTitle: "Enable traffic shaping?",
		PromptDesc: "Create the BN workload network-policy plane (inet weaver classification) and tc HTB " +
			"traffic shaping, and install the traffic-shaper daemon. Opt-in, default No — choose Yes " +
			"to get all three.",
		ContentFlags: []string{
			FlagNameEgressInterface, FlagNameLinkRate, FlagNameShape,
			FlagDaemonBin().Name, FlagDaemonVersion().Name,
		},
	}
}
