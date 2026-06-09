// SPDX-License-Identifier: Apache-2.0

package prompt

import (
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"

	"github.com/hashgraph/solo-weaver/pkg/sanity"
)

// ── private per-field prompt builders ────────────────────────────────────────

func daemonNodeIDInputPrompt(eff string, target *string) InputPrompt {
	return InputPrompt{
		FlagName:       "node-id",
		Title:          "Hedera Node ID",
		Description:    `Identifier for this consensus node (e.g. "0.0.3")`,
		Placeholder:    "0.0.3",
		EffectiveValue: eff,
		Target:         target,
		Validate: func(s string) error {
			if s == "" {
				return errorx.IllegalArgument.New("node-id cannot be empty")
			}
			return nil
		},
	}
}

func daemonOrbitInputPrompt(eff string, target *string) InputPrompt {
	return InputPrompt{
		FlagName:       "orbit",
		Title:          "Orbit Namespace",
		Description:    "Kubernetes namespace where NetworkUpgradeExecute CRs are watched",
		Placeholder:    "hedera-network",
		EffectiveValue: eff,
		Target:         target,
		Validate: func(s string) error {
			if s == "" {
				return errorx.IllegalArgument.New("orbit cannot be empty")
			}
			return sanity.ValidateIdentifier(s)
		},
	}
}

func daemonUpgradeDirInputPrompt(eff string, target *string) InputPrompt {
	const defaultUpgradeDir = "/opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current"
	if eff == "" {
		eff = defaultUpgradeDir
	}
	return InputPrompt{
		FlagName:       "upgrade-dir",
		Title:          "Upgrade Staging Directory",
		Description:    "Path to the CN upgrade staging directory (leave unchanged to use the default)",
		Placeholder:    defaultUpgradeDir,
		EffectiveValue: eff,
		Target:         target,
		Validate: func(s string) error {
			if s == "" {
				return nil // empty means "use default" — validated at workflow time
			}
			_, err := sanity.SanitizePath(s)
			return err
		},
	}
}

// ── Public prompt builder ─────────────────────────────────────────────────────

// DaemonInstallInputTargets holds pointers to the flag variables for the daemon
// install prompts. Kept as a struct to avoid a long parameter list.
type DaemonInstallInputTargets struct {
	NodeID     *string
	Orbit      *string
	UpgradeDir *string
}

// RunDaemonInstallPrompts presents interactive text-input prompts for the
// daemon service install command. Only prompts for fields that were not already
// set on the command line. upgrade-dir is optional so the user may leave it
// blank to accept the daemon's compile-time default.
//
// Prompts are presented as a single form so the user can tab through all fields
// at once. Chosen values are collected into cv for summary printing.
func RunDaemonInstallPrompts(cmd *cobra.Command, targets DaemonInstallInputTargets, cv *ChosenValues) error {
	prompts := []InputPrompt{
		daemonNodeIDInputPrompt(*targets.NodeID, targets.NodeID),
		daemonOrbitInputPrompt(*targets.Orbit, targets.Orbit),
		daemonUpgradeDirInputPrompt(*targets.UpgradeDir, targets.UpgradeDir),
	}
	return RunInputPrompts(cmd, prompts, cv)
}
