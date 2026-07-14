// SPDX-License-Identifier: Apache-2.0

package common

import (
	"strconv"
	"strings"

	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/internal/ui/prompt"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

// Host-firewall flag names, shared by every block-node command that can apply
// the node-level host firewall (install, reconfigure, upgrade). Kept in one
// place so the flag registration and the resolver never drift.
const (
	FlagNameFirewallEnabled = "firewall-enabled"
	FlagNameMgmtCIDRs       = "mgmt-cidrs"
	FlagNameSSHPort         = "ssh-port"
	FlagNamePodCIDR         = "pod-cidr"
	FlagNameInClusterPorts  = "in-cluster-ports"
)

// ValidateHostFirewallFlags validates format-sensitive host-firewall flags that
// are only caught in ResolveHostFirewallConfig — which runs after the interactive
// wizard. Call this at the top of RunE, before prepareBlocknodeInputs, so
// operators get immediate feedback for invalid CLI inputs.
//
// Only flags explicitly Changed on the CLI are checked; un-set flags fall back
// to config / built-in defaults and are validated later inside
// ResolveHostFirewallConfig as always.
func ValidateHostFirewallFlags(cmd *cobra.Command) error {
	if cmd.Flags().Changed(FlagNameSSHPort) {
		port, _ := cmd.Flags().GetInt(FlagNameSSHPort)
		if err := sanity.ValidatePort(strconv.Itoa(port)); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid --%s %d", FlagNameSSHPort, port)
		}
	}
	if cmd.Flags().Changed(FlagNameInClusterPorts) {
		ports, _ := cmd.Flags().GetIntSlice(FlagNameInClusterPorts)
		for _, p := range ports {
			if err := sanity.ValidatePort(strconv.Itoa(p)); err != nil {
				return errorx.IllegalArgument.Wrap(err, "invalid --%s %d", FlagNameInClusterPorts, p)
			}
		}
	}
	if cmd.Flags().Changed(FlagNameMgmtCIDRs) {
		cidrs, _ := cmd.Flags().GetStringSlice(FlagNameMgmtCIDRs)
		for _, cidr := range normalizeCIDRs(cidrs) {
			if err := sanity.ValidateIPv4CIDR(cidr); err != nil {
				return errorx.IllegalArgument.Wrap(err, "invalid --%s %q", FlagNameMgmtCIDRs, cidr)
			}
		}
	}
	if cmd.Flags().Changed(FlagNamePodCIDR) {
		podCIDR, _ := cmd.Flags().GetString(FlagNamePodCIDR)
		if err := sanity.ValidateIPv4CIDR(strings.TrimSpace(podCIDR)); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid --%s %q", FlagNamePodCIDR, podCIDR)
		}
	}
	return nil
}

// RegisterHostFirewallFlags registers the node-level host firewall flags on cmd.
// The values are read back by name in ResolveHostFirewallConfig (no bound vars),
// so the same registration works for any command that provisions a host.
func RegisterHostFirewallFlags(cmd *cobra.Command) {
	cmd.Flags().Bool(FlagNameFirewallEnabled, true,
		"Apply the node-level host firewall (inet host table: SSH/mgmt allowlist, ICMP policy, in-cluster ports). "+
			"Disable for hosts managed by an external firewall.")
	cmd.Flags().StringSlice(FlagNameMgmtCIDRs, nil,
		"SSH/management allowlist CIDRs for the node host firewall (comma-separated or repeated). Empty skips the host firewall.")
	cmd.Flags().Int(FlagNameSSHPort, firewall.DefaultSSHPort,
		"SSH/management TCP port allowed from --mgmt-cidrs by the node host firewall")
	cmd.Flags().String(FlagNamePodCIDR, models.DefaultClusterPodCIDR,
		"Pod CIDR allowed to reach the in-cluster host-service ports (defaults to the cluster pod subnet)")
	cmd.Flags().IntSlice(FlagNameInClusterPorts, firewall.DefaultInClusterPorts,
		"Host-service ports reachable from the pod CIDR by the node host firewall (comma-separated)")
}

// ResolveHostFirewallConfig determines the effective host firewall configuration
// (enabled, management CIDR allowlist, SSH port, pod CIDR, in-cluster
// host-service ports) and applies it to the global config so the
// NetworkFirewallCreate step (wired into the block-node install/reconfigure/
// upgrade workflows) can render the inet host table. Precedence per value:
// CLI flag > interactive prompt > config file > built-in default. When the
// session is interactive, any value not supplied on the CLI is presented as a
// pre-filled prompt the operator can confirm with Enter. An empty management
// allowlist is allowed — the step then skips firewall creation rather than
// rendering a lock-out (default-drop) ruleset.
//
// When cv is non-nil the prompted values are recorded into it and no separate
// summary is printed — the caller is responsible for printing the unified
// summary after all prompt sections complete. When cv is nil a local collector
// is used and printed as "Host Firewall" immediately.
//
// It requires RegisterHostFirewallFlags to have been called on cmd.
func ResolveHostFirewallConfig(cmd *cobra.Command, args []string, cv *prompt.ChosenValues) error {
	force, err := FlagForce().Value(cmd, args)
	if err != nil {
		return errorx.IllegalArgument.Wrap(err, "failed to get %s flag", FlagForce().Name)
	}

	cfg := config.Get().Host
	firewallEnabled := effectiveBool(cmd, FlagNameFirewallEnabled, !cfg.Disabled)

	// Prompt for the enable/disable choice only when it wasn't already decided
	// on the CLI. Declining here skips the allowlist/port prompts below entirely
	// — there's nothing left to ask once the firewall itself is turned off.
	if prompt.ShouldPrompt(force) && !cmd.Flags().Changed(FlagNameFirewallEnabled) {
		enabled, err := prompt.RunConfirm(
			"Enable host firewall?",
			"Apply the node-level inet host firewall (SSH/mgmt allowlist, ICMP policy, in-cluster ports). "+
				"Choose No to skip entirely, e.g. for hosts managed by an external firewall.",
			firewallEnabled,
		)
		if err != nil {
			return err
		}
		firewallEnabled = enabled
	}

	// An explicit opt-out skips resolving/prompting for the allowlist/port
	// fields entirely — there's nothing to ask once the firewall itself is
	// disabled. Other fields are preserved (not wiped) so re-enabling later
	// doesn't require re-entering them.
	if !firewallEnabled {
		hostCfg := cfg
		hostCfg.Disabled = true
		config.OverrideHostConfig(hostCfg)
		return nil
	}

	// Seed each prompt target with the effective value: the CLI flag when the
	// operator set it, else the config value, else the built-in default. The
	// prompt layer skips any flag already set on the CLI, leaving these seeds
	// intact, so the same strings are parsed whether or not a prompt ran.
	mgmtStr := effectiveCSV(cmd, FlagNameMgmtCIDRs, cfg.ManagementCIDRs)
	sshStr := effectiveInt(cmd, FlagNameSSHPort, cfg.SSHPort, firewall.DefaultSSHPort)
	portsStr := effectiveIntCSV(cmd, FlagNameInClusterPorts, cfg.InClusterPorts, firewall.DefaultInClusterPorts)
	podStr := effectiveStr(cmd, FlagNamePodCIDR, cfg.PodCIDR, models.DefaultClusterPodCIDR)

	if prompt.ShouldPrompt(force) {
		localCV := cv
		if localCV == nil {
			localCV = prompt.NewChosenValues()
		}
		if err := prompt.RunInputPrompts(cmd, []prompt.InputPrompt{
			prompt.MgmtCIDRsInputPrompt(mgmtStr, &mgmtStr),
			prompt.SSHPortInputPrompt(sshStr, &sshStr),
			prompt.PodCIDRInputPrompt(podStr, &podStr),
			prompt.InClusterPortsInputPrompt(portsStr, &portsStr),
		}, localCV); err != nil {
			return err
		}
		if cv == nil {
			localCV.Print("Host Firewall")
		}
	}

	sshPort, err := strconv.Atoi(strings.TrimSpace(sshStr))
	if err != nil {
		return errorx.IllegalArgument.New("invalid --%s %q", FlagNameSSHPort, sshStr)
	}
	ports, err := prompt.ParsePortList(portsStr)
	if err != nil {
		return errorx.IllegalArgument.Wrap(err, "invalid --%s", FlagNameInClusterPorts)
	}

	hostCfg := models.HostConfig{
		ManagementCIDRs: normalizeCIDRs(strings.Split(mgmtStr, ",")),
		SSHPort:         sshPort,
		PodCIDR:         strings.TrimSpace(podStr),
		InClusterPorts:  ports,
		Disabled:        false,
	}
	if err := hostCfg.Validate(); err != nil {
		return errorx.IllegalArgument.Wrap(err, "invalid host firewall configuration")
	}
	config.OverrideHostConfig(hostCfg)
	return nil
}

// effectiveBool returns the effective value for a bool flag: the flag when
// explicitly set, else cfgVal (which already carries the correct "unset"
// default via HostConfig.Disabled's negative polarity).
func effectiveBool(cmd *cobra.Command, name string, cfgVal bool) bool {
	if cmd.Flags().Changed(name) {
		v, _ := cmd.Flags().GetBool(name)
		return v
	}
	return cfgVal
}

// effectiveCSV returns the effective comma-joined value for a StringSlice flag:
// the flag when explicitly set, else the config value.
func effectiveCSV(cmd *cobra.Command, name string, cfgVal []string) string {
	if cmd.Flags().Changed(name) {
		v, _ := cmd.Flags().GetStringSlice(name)
		return strings.Join(v, ",")
	}
	return strings.Join(cfgVal, ",")
}

// effectiveStr returns the effective value for a string flag: the flag when set,
// else the config value when non-empty, else the built-in default.
func effectiveStr(cmd *cobra.Command, name, cfgVal, def string) string {
	if cmd.Flags().Changed(name) {
		v, _ := cmd.Flags().GetString(name)
		return v
	}
	if cfgVal != "" {
		return cfgVal
	}
	return def
}

// effectiveInt returns the effective value for an int flag as a string: the flag
// when set, else the config value when non-zero, else the built-in default.
func effectiveInt(cmd *cobra.Command, name string, cfgVal, def int) string {
	if cmd.Flags().Changed(name) {
		v, _ := cmd.Flags().GetInt(name)
		return strconv.Itoa(v)
	}
	if cfgVal != 0 {
		return strconv.Itoa(cfgVal)
	}
	return strconv.Itoa(def)
}

// effectiveIntCSV returns the effective comma-joined value for an IntSlice flag:
// the flag when set, else the config value when non-empty, else the default.
func effectiveIntCSV(cmd *cobra.Command, name string, cfgVal, def []int) string {
	switch {
	case cmd.Flags().Changed(name):
		v, _ := cmd.Flags().GetIntSlice(name)
		return joinInts(v)
	case len(cfgVal) > 0:
		return joinInts(cfgVal)
	default:
		return joinInts(def)
	}
}

func joinInts(in []int) string {
	parts := make([]string, len(in))
	for i, n := range in {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

// normalizeCIDRs trims whitespace and drops empty entries from a CIDR list so
// a trailing comma or blank prompt entry does not produce an invalid "" CIDR.
func normalizeCIDRs(in []string) []string {
	out := make([]string, 0, len(in))
	for _, c := range in {
		c = strings.TrimSpace(c)
		if c != "" {
			out = append(out, c)
		}
	}
	return out
}
