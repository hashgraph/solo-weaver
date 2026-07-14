// SPDX-License-Identifier: Apache-2.0

package common

import (
	"github.com/hashgraph/solo-weaver/internal/network/shape"
	"github.com/hashgraph/solo-weaver/internal/ui/prompt"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

// FlagNameEgressInterface and FlagNameLinkRate are the CLI flag names for
// the tc-egress NIC and link-rate settings, shared by every block-node command
// that renders the tc-egress script (install, reconfigure).
const (
	FlagNameEgressInterface = "egress-interface"
	FlagNameLinkRate        = "link-rate"
)

// RegisterEgressFlags registers the egress-interface and link-rate flags on
// cmd, binding them to the caller-supplied variables. The values are read back
// by ResolveEgressConfig.
func RegisterEgressFlags(cmd *cobra.Command, egressInterface, linkRate *string) {
	cmd.Flags().StringVar(egressInterface, FlagNameEgressInterface, "",
		"Physical NIC for the $EGRESS HTB traffic-shaper hierarchy (e.g. eth0). "+
			"Auto-detected from the default route when omitted; use this flag to override on multi-NIC hosts.")
	cmd.Flags().StringVar(linkRate, FlagNameLinkRate, "",
		"NIC line rate in tc-style format (e.g. 1gbit, 100mbit). "+
			"Auto-detected from sysfs at boot when omitted; baked into the script when set.")
}

// ValidateEgressFlags rejects a set --link-rate value that is not a valid
// tc-style rate. Call this before any interactive prompts so the operator gets
// an immediate error rather than sitting through the whole wizard first.
//
// It requires RegisterEgressFlags to have been called on cmd.
func ValidateEgressFlags(cmd *cobra.Command, linkRate string) error {
	if cmd.Flags().Changed(FlagNameLinkRate) && linkRate != "" {
		if _, ok := shape.ParseSpeedMbit(linkRate); !ok {
			return errorx.IllegalArgument.New(
				"invalid --%s %q: must be a tc-style rate (e.g. 1gbit, 100mbit)",
				FlagNameLinkRate, linkRate)
		}
	}
	return nil
}

// ResolveEgressConfig detects the egress NIC from the default route and the
// link speed from sysfs, then prompts for both values when the session is
// interactive and the flags were not supplied on the CLI. egressInterface and
// linkRate are updated in-place by the prompts.
//
// When cv is non-nil the prompted values are recorded into it and no separate
// summary is printed — the caller is responsible for printing the unified
// summary after all prompt sections complete. When cv is nil a local collector
// is used and printed as "Egress" immediately.
//
// It requires RegisterEgressFlags to have been called on cmd.
func ResolveEgressConfig(
	cmd *cobra.Command,
	args []string,
	cv *prompt.ChosenValues,
	egressInterface, linkRate *string,
) error {
	force, err := FlagForce().Value(cmd, args)
	if err != nil {
		return errorx.IllegalArgument.Wrap(err, "failed to get %s flag", FlagForce().Name)
	}

	if !prompt.ShouldPrompt(force) {
		return nil
	}

	// Detect the egress NIC to use as the prompt default.
	detectedNIC, _ := shape.DetectEgressInterface()

	// Speed hint from sysfs: prefer the already-set flag value over the
	// auto-detected one so the hint reflects the NIC the operator chose.
	effectiveNIC := detectedNIC
	if cmd.Flags().Changed(FlagNameEgressInterface) {
		effectiveNIC = *egressInterface
	}
	var speedHint string
	if effectiveNIC != "" {
		if mbit, ok := shape.ReadLinkSpeedMbit(effectiveNIC); ok {
			speedHint = shape.FormatSpeedHint(mbit)
		}
	}

	localCV := cv
	if localCV == nil {
		localCV = prompt.NewChosenValues()
	}
	if err := prompt.RunInputPrompts(cmd, []prompt.InputPrompt{
		prompt.EgressInterfaceInputPrompt(detectedNIC, speedHint, egressInterface),
		prompt.LinkRateInputPrompt(speedHint, linkRate),
	}, localCV); err != nil {
		return err
	}
	if cv == nil {
		localCV.Print("Egress")
	}
	return nil
}
