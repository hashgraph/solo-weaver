// SPDX-License-Identifier: Apache-2.0

package common

import (
	"strings"

	"github.com/hashgraph/solo-weaver/internal/network/shape"
	"github.com/hashgraph/solo-weaver/internal/ui/prompt"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

// FlagNameEgressInterface and FlagNameLinkRate are the CLI flag names for
// the bandwidth-shaper NIC and link-rate settings, shared by every block-node command
// that renders the bandwidth-shaper script (install, reconfigure).
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
		"NIC line rate in tc-style format (e.g. 1gbit, 100mbit), or \"auto\" to detect and store the speed at install time. "+
			"Auto-detected from sysfs at each boot when omitted; written as explicit proportional class rates when set.")
}

// ValidateEgressFlags rejects a set --link-rate value that is not a valid
// tc-style rate. Call this before any interactive prompts so the operator gets
// an immediate error rather than sitting through the whole wizard first.
//
// It requires RegisterEgressFlags to have been called on cmd.
func ValidateEgressFlags(cmd *cobra.Command, linkRate string) error {
	if cmd.Flags().Changed(FlagNameLinkRate) && linkRate != "" {
		if strings.EqualFold(strings.TrimSpace(linkRate), "auto") {
			return nil
		}
		if _, ok := shape.ParseSpeedMbit(linkRate); !ok {
			return errorx.IllegalArgument.New(
				"invalid --%s %q: must be a tc-style rate (e.g. 1gbit, 100mbit) or \"auto\"",
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
// egressInterface and linkRate double as seed inputs: when either already holds
// a non-empty value (e.g. `block node reconfigure` pre-seeding it from the
// persisted BlockNodeState.Shaping last-chosen values), that value becomes the
// prompt's pre-filled default so a default-accept keeps the operator's
// previously-configured NIC / link rate instead of silently reverting to
// auto-detection. On a fresh `install` both are empty, so the detected NIC and
// sysfs link speed are used — the original behaviour.
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

	// Detect the egress NIC to use as the prompt default, unless a value was
	// already seeded (persisted last-chosen NIC on reconfigure), which wins.
	detectedNIC, _ := shape.DetectEgressInterface()
	egressDefault := detectedNIC
	if *egressInterface != "" {
		egressDefault = *egressInterface
	}

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

	// Seed the link-rate prompt from the persisted last-chosen rate (when the
	// caller pre-seeded linkRate, e.g. reconfigure reading BlockNodeState.Shaping)
	// so a default-accept keeps the operator's configured rate instead of
	// reverting to the NIC's raw sysfs line speed. Falls back to the sysfs hint
	// on a fresh install where nothing was chosen yet.
	linkRateDefault := speedHint
	if *linkRate != "" {
		linkRateDefault = *linkRate
	}

	localCV := cv
	if localCV == nil {
		localCV = prompt.NewChosenValues()
	}
	if err := prompt.RunInputPrompts(cmd, []prompt.InputPrompt{
		prompt.EgressInterfaceInputPrompt(egressDefault, speedHint, egressInterface),
		prompt.LinkRateInputPrompt(linkRateDefault, linkRate),
	}, localCV); err != nil {
		return err
	}
	if cv == nil {
		localCV.Print("Egress")
	}
	return nil
}
