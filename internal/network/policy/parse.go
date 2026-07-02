// SPDX-License-Identifier: Apache-2.0

package policy

import "regexp"

// rePodCIDR matches the literal CIDR every rendered --stamp rule starts with
// (`ip daddr <PODCIDR> ...` ingress, `ip saddr <PODCIDR> ...` egress/reply-stamp
// forward -- see renderStampRule). A `\d+\.\d+\.\d+\.\d+/\d+` literal never
// matches a deny rule's `ip saddr/daddr @<name> drop`, which references a set
// by name, not an inline CIDR.
var rePodCIDR = regexp.MustCompile(`ip (?:daddr|saddr) (\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}/\d{1,2})\b`)

// ExtractPodCIDR recovers the pod CIDR last used to render network-weaver.nft.
// Mirrors internal/network/firewall's Parse(): it understands only the exact
// format Render produces, not a general nft parser, and exists so a caller
// that doesn't supply --pod-cidr (as --deny never does) can still correctly
// re-render unchanged --stamp siblings using the value they were already
// rendered with, instead of requiring every call to re-supply or re-detect a
// value that's effectively a deployment-wide constant. Returns "" if none is
// found (e.g. a deny-only chain, or the file doesn't exist yet).
func ExtractPodCIDR(content string) string {
	if m := rePodCIDR.FindStringSubmatch(content); m != nil {
		return m[1]
	}
	return ""
}
