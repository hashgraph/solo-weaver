// SPDX-License-Identifier: Apache-2.0

package prompt

import (
	"strings"

	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"

	daemon "github.com/hashgraph/solo-weaver/internal/daemon"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
)

// ── Component name constants ──────────────────────────────────────────────────

// Component name constants are the canonical identifiers used in --components,
// daemon.yaml, and all component-branching logic. Always reference these
// constants — never use raw string literals for component names.
const (
	ComponentConsensusNode = "consensus-node"
	ComponentBlockNode     = "block-node"
)

// knownComponents is the ordered registry of all supported daemon components.
// To add a new component: append one entry here. No other parsing or prompting
// code needs to change.
var knownComponents = []struct {
	name        string
	description string
	defaultOn   bool
}{
	{ComponentConsensusNode, "Monitor NetworkUpgradeExecute CRs for consensus-node upgrades", true},
	{ComponentBlockNode, "Monitor block-node component (--bn-orbit required)", false},
}

// ── ComponentSet ─────────────────────────────────────────────────────────────

// ComponentSet holds the set of daemon components selected by the operator.
// Use Has() to test membership; Empty() to detect nothing selected.
type ComponentSet map[string]struct{}

// Has returns true if name was selected.
func (cs ComponentSet) Has(name string) bool {
	_, ok := cs[name]
	return ok
}

// Empty returns true when no component was selected.
func (cs ComponentSet) Empty() bool { return len(cs) == 0 }

// String serialises the set back to a comma-separated list in knownComponents
// order — suitable for writing back into the --components flag variable.
func (cs ComponentSet) String() string {
	var parts []string
	for _, kc := range knownComponents {
		if cs.Has(kc.name) {
			parts = append(parts, kc.name)
		}
	}
	return strings.Join(parts, ",")
}

// ── Component parsing and prompting ──────────────────────────────────────────

// ParseComponentsFlag splits a comma-separated --components value into a
// ComponentSet. Unknown names are silently ignored so a newer daemon.yaml
// opened by an older CLI does not error.
func ParseComponentsFlag(raw string) ComponentSet {
	cs := make(ComponentSet)
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		if name != "" {
			cs[name] = struct{}{}
		}
	}
	return cs
}

// promptForComponents asks the operator which components to enable via one
// confirm prompt per knownComponents entry. rawComponents is updated so the
// caller can persist the selection back into the --components flag variable.
func promptForComponents(rawComponents *string) (ComponentSet, error) {
	cs := make(ComponentSet)
	for _, kc := range knownComponents {
		chosen, err := RunConfirm("Enable "+kc.name+" component?", kc.description, kc.defaultOn)
		if err != nil {
			return nil, err
		}
		if chosen {
			cs[kc.name] = struct{}{}
		}
	}
	if rawComponents != nil {
		*rawComponents = cs.String()
	}
	return cs, nil
}

// ── Private per-field prompt builders ────────────────────────────────────────

func daemonNodeIDInputPrompt(eff string, target *string) InputPrompt {
	return InputPrompt{
		FlagName:       "cn-node-id",
		Title:          "Consensus Node ID",
		Description:    `Identifier for this consensus node (e.g. "0.0.3")`,
		Placeholder:    "0.0.3",
		EffectiveValue: eff,
		Target:         target,
		Validate: func(s string) error {
			if s == "" {
				return errorx.IllegalArgument.New("cn-node-id cannot be empty")
			}
			return nil
		},
	}
}

func daemonCNOrbitInputPrompt(eff string, target *string) InputPrompt {
	return InputPrompt{
		FlagName:       "cn-orbit",
		Title:          "Consensus Node Orbit Namespace",
		Description:    "Kubernetes namespace where consensus-node NetworkUpgradeExecute CRs are watched",
		Placeholder:    "hedera-network",
		EffectiveValue: eff,
		Target:         target,
		Validate: func(s string) error {
			if s == "" {
				return errorx.IllegalArgument.New("cn-orbit cannot be empty")
			}
			return sanity.ValidateIdentifier(s)
		},
	}
}

func daemonCNUpgradeDirInputPrompt(eff string, target *string) InputPrompt {
	const defaultUpgradeDir = "/opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current"
	if eff == "" {
		eff = defaultUpgradeDir
	}
	return InputPrompt{
		FlagName:       "cn-upgrade-dir",
		Title:          "CN Upgrade Staging Directory",
		Description:    "Path to the consensus-node upgrade staging directory (leave unchanged to use the default)",
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

func daemonBNOrbitInputPrompt(eff string, target *string) InputPrompt {
	return InputPrompt{
		FlagName:       "bn-orbit",
		Title:          "Block Node Orbit Namespace",
		Description:    "Kubernetes namespace for the block-node component",
		Placeholder:    "hedera-block-node",
		EffectiveValue: eff,
		Target:         target,
		Validate: func(s string) error {
			if s == "" {
				return errorx.IllegalArgument.New("bn-orbit cannot be empty")
			}
			return sanity.ValidateIdentifier(s)
		},
	}
}

// ── Public prompt builder ─────────────────────────────────────────────────────

// DaemonInstallInputTargets carries flag-variable pointers for the daemon
// install prompts. ComponentsRaw is both read (detect pre-set flag) and
// written (record interactive choices).
type DaemonInstallInputTargets struct {
	// ComponentsRaw is the --components flag variable pointer.
	ComponentsRaw *string

	// Per-component field pointers — nil when the component was not pre-selected
	// (they are wired in after the component-selection step).
	CNNodeID     *string
	CNOrbit      *string
	CNUpgradeDir *string
	BNOrbit      *string
}

// RunDaemonInstallPrompts presents interactive prompts for the daemon service
// install command. It first resolves which components to enable (from
// --components or via interactive confirm prompts), then presents the relevant
// per-component input fields.
//
// cfg is updated in-place to reflect newly-enabled components and entered
// values. Chosen values are collected into cv for summary printing.
func RunDaemonInstallPrompts(
	cmd *cobra.Command,
	cfg *daemon.DaemonConfig,
	targets DaemonInstallInputTargets,
	paths models.WeaverPaths,
	cv *ChosenValues,
) error {
	// Step 1: resolve which components to enable.
	var cs ComponentSet
	if targets.ComponentsRaw != nil && *targets.ComponentsRaw != "" {
		cs = ParseComponentsFlag(*targets.ComponentsRaw)
	} else {
		var err error
		cs, err = promptForComponents(targets.ComponentsRaw)
		if err != nil {
			return err
		}
	}

	// At least one component is required — RBAC and kubeconfigs are only
	// provisioned for selected components.
	if cs.Empty() {
		return errorx.IllegalArgument.New(
			"no daemon components selected; enable at least one component")
	}

	// Step 2: wire up consensus-node component if selected.
	if cs.Has(ComponentConsensusNode) && cfg.Components.ConsensusNode == nil {
		c := daemon.ConsensusNodeComponentConfig{
			Enabled:    true,
			Kubeconfig: paths.DaemonCNKubeconfigPath,
			Monitors:   daemon.ConsensusNodeMonitors{Upgrade: true, Migration: true},
		}
		cfg.Components.ConsensusNode = &c
		targets.CNNodeID = &cfg.Components.ConsensusNode.NodeID
		targets.CNOrbit = &cfg.Components.ConsensusNode.Orbit
		targets.CNUpgradeDir = &cfg.Components.ConsensusNode.UpgradeDir
	}

	// Step 3: prompt for consensus-node fields.
	if cs.Has(ComponentConsensusNode) && cfg.Components.ConsensusNode != nil {
		cn := cfg.Components.ConsensusNode
		nodeIDTarget := targets.CNNodeID
		if nodeIDTarget == nil {
			nodeIDTarget = &cn.NodeID
		}
		orbitTarget := targets.CNOrbit
		if orbitTarget == nil {
			orbitTarget = &cn.Orbit
		}
		upgradeDirTarget := targets.CNUpgradeDir
		if upgradeDirTarget == nil {
			upgradeDirTarget = &cn.UpgradeDir
		}
		cnPrompts := []InputPrompt{
			daemonNodeIDInputPrompt(*nodeIDTarget, nodeIDTarget),
			daemonCNOrbitInputPrompt(*orbitTarget, orbitTarget),
			daemonCNUpgradeDirInputPrompt(*upgradeDirTarget, upgradeDirTarget),
		}
		if err := RunInputPrompts(cmd, cnPrompts, cv); err != nil {
			return err
		}
	}

	// Step 4: wire up block-node component if selected.
	if cs.Has(ComponentBlockNode) && cfg.Components.BlockNode == nil {
		c := daemon.BlockNodeComponentConfig{
			Enabled:    true,
			Kubeconfig: paths.DaemonBNKubeconfigPath,
			Monitors:   daemon.BlockNodeMonitors{TrafficShaper: true},
		}
		cfg.Components.BlockNode = &c
		targets.BNOrbit = &cfg.Components.BlockNode.Orbit
	}

	// Step 5: prompt for block-node fields.
	if cs.Has(ComponentBlockNode) && cfg.Components.BlockNode != nil {
		bn := cfg.Components.BlockNode
		orbitTarget := targets.BNOrbit
		if orbitTarget == nil {
			orbitTarget = &bn.Orbit
		}
		bnPrompts := []InputPrompt{
			daemonBNOrbitInputPrompt(*orbitTarget, orbitTarget),
		}
		if err := RunInputPrompts(cmd, bnPrompts, cv); err != nil {
			return err
		}
	}

	return nil
}
