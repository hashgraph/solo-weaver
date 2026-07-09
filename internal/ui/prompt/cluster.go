// SPDX-License-Identifier: Apache-2.0

package prompt

import (
	"strconv"
	"strings"

	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
)

// validateMgmtCIDRs validates a comma-separated management CIDR allowlist.
// An empty value is allowed — it means "no host firewall", which the install
// step handles by skipping firewall creation (rather than rendering a lock-out
// ruleset). Each non-empty entry must be a valid CIDR.
func validateMgmtCIDRs(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	for _, c := range strings.Split(s, ",") {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if err := sanity.ValidateIPv4CIDR(c); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid management CIDR %q", c)
		}
	}
	return nil
}

// validatePodCIDR validates the pod CIDR. Empty is allowed (the in-cluster
// host-service ports rule is then omitted); a non-empty value must be a CIDR.
func validatePodCIDR(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if err := sanity.ValidateIPv4CIDR(s); err != nil {
		return errorx.IllegalArgument.Wrap(err, "invalid pod CIDR %q", s)
	}
	return nil
}

// validateSSHPort validates the SSH/management port. It is required (the table's
// SSH allow rule always renders a port) and must be a valid TCP port.
func validateSSHPort(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return errorx.IllegalArgument.New("SSH port cannot be empty")
	}
	if err := sanity.ValidatePort(s); err != nil {
		return errorx.IllegalArgument.Wrap(err, "invalid SSH port %q", s)
	}
	return nil
}

// validateInClusterPorts validates a comma-separated list of in-cluster
// host-service ports. Empty is allowed (no ports opened); each non-empty entry
// must be a valid TCP port.
func validateInClusterPorts(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if err := sanity.ValidatePort(p); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid in-cluster port %q", p)
		}
	}
	return nil
}

// MgmtCIDRsInputPrompt returns the interactive prompt for the host firewall's
// SSH/management allowlist. eff is the effective value (from flag/config) shown
// as the pre-filled suggestion; target receives the comma-separated result.
func MgmtCIDRsInputPrompt(eff string, target *string) InputPrompt {
	return InputPrompt{
		FlagName:       "mgmt-cidrs",
		Title:          "Management CIDRs (host firewall)",
		Description:    "SSH/management allowlist for the node firewall (comma-separated CIDRs). Leave empty to skip the host firewall.",
		Placeholder:    "10.0.0.0/8,192.168.0.0/16",
		EffectiveValue: eff,
		Target:         target,
		Validate:       validateMgmtCIDRs,
	}
}

// SSHPortInputPrompt returns the interactive prompt for the SSH/management port.
func SSHPortInputPrompt(eff string, target *string) InputPrompt {
	return InputPrompt{
		FlagName:       "ssh-port",
		Title:          "SSH port (host firewall)",
		Description:    "TCP port accepted from the management allowlist for SSH/management access.",
		Placeholder:    eff,
		EffectiveValue: eff,
		Target:         target,
		Validate:       validateSSHPort,
	}
}

// InClusterPortsInputPrompt returns the interactive prompt for the in-cluster
// host-service ports reachable from the pod CIDR.
func InClusterPortsInputPrompt(eff string, target *string) InputPrompt {
	return InputPrompt{
		FlagName:       "in-cluster-ports",
		Title:          "In-cluster host-service ports (host firewall)",
		Description:    "Host-service ports reachable from the pod CIDR (comma-separated). Defaults cover kube-apiserver, cilium, metallb, and kubelet.",
		Placeholder:    eff,
		EffectiveValue: eff,
		Target:         target,
		Validate:       validateInClusterPorts,
	}
}

// PodCIDRInputPrompt returns the interactive prompt for the pod CIDR that is
// allowed to reach the in-cluster host-service ports.
func PodCIDRInputPrompt(eff string, target *string) InputPrompt {
	return InputPrompt{
		FlagName:       "pod-cidr",
		Title:          "Pod CIDR (host firewall)",
		Description:    "Pod source range allowed to reach the in-cluster host-service ports. Defaults to the cluster pod subnet. Leave empty to omit the rule.",
		Placeholder:    eff,
		EffectiveValue: eff,
		Target:         target,
		Validate:       validatePodCIDR,
	}
}

// ParsePortList parses a comma-separated port list into ints, skipping blanks.
// It assumes the input already passed validateInClusterPorts.
func ParsePortList(s string) ([]int, error) {
	var out []int
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, errorx.IllegalArgument.Wrap(err, "invalid port %q", p)
		}
		out = append(out, n)
	}
	return out, nil
}
