// SPDX-License-Identifier: Apache-2.0

package policy

import "regexp"

// rePodCIDR / rePodCIDR6 match the literal CIDR every rendered --stamp rule
// starts with (`ip daddr <PODCIDR> ...` ingress, `ip saddr <PODCIDR> ...`
// egress/reply-stamp forward, and the `ip6` variants -- see renderStampRule). An
// inline-CIDR literal never matches a deny rule's `ip saddr/daddr @<name> drop`,
// which references a set by name, not an inline CIDR. The v6 pattern deliberately
// excludes '@' so it never matches a `@<name>6` set reference.
var (
	rePodCIDR  = regexp.MustCompile(`ip (?:daddr|saddr) (\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}/\d{1,2})\b`)
	rePodCIDR6 = regexp.MustCompile(`ip6 (?:daddr|saddr) ([0-9A-Fa-f:]+/\d{1,3})\b`)
)

// ExtractPodCIDRs recovers the pod CIDR(s) last used to render network-weaver.nft
// (IPv4 first, then IPv6 if present). Mirrors internal/network/firewall's Parse():
// it understands only the exact format Render produces, not a general nft parser,
// and exists so a caller that doesn't supply --pod-cidr (as --deny never does)
// can still correctly re-render unchanged --stamp siblings using the value they
// were already rendered with, instead of requiring every call to re-supply or
// re-detect a value that's effectively a deployment-wide constant. Returns nil
// if none is found (e.g. a deny-only chain, or the file doesn't exist yet).
func ExtractPodCIDRs(content string) []string {
	var out []string
	if m := rePodCIDR.FindStringSubmatch(content); m != nil {
		out = append(out, m[1])
	}
	if m := rePodCIDR6.FindStringSubmatch(content); m != nil {
		out = append(out, m[1])
	}
	return out
}
