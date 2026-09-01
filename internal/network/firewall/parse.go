// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"regexp"
	"strings"

	"github.com/joomcode/errorx"
)

// Parse recovers the three reserved blocks of a Table from a rendered
// network-weaver-host-firewall.nft artifact. It understands only the exact
// formats this package renders — it is not a general nft parser.
//
// It is the fallback path, not the normal one: the persisted YAML config is the
// source of truth for the mutating verbs (see Manager.load). Parse exists so a
// host provisioned before named allow rules existed — or one whose config file
// was lost — still yields its management allowlist rather than an error that
// leaves the operator with no way to add their address back. Named allow rules
// are deliberately NOT recovered here: reverse-engineering arbitrary named rules
// out of nft syntax would be fragile in exactly the situation where being wrong
// costs the most. Recovering management access is the goal; the allow rules are
// not recovered at all — neither their existence nor their membership — so they
// must be re-declared with `network firewall create-allow-rule` and then
// re-populated with `network firewall add`.
//
// Domain names in mgmt are lost here too, and worse than in the allow rules
// that already carry them: the rendered artifact only ever holds resolved
// addresses, so a mgmt entry authored as a name comes back as the `/32`s it
// last pointed at. The next mutation then persists those into the config as
// the new source of truth, replacing the name with a frozen snapshot. An allow
// rule's own name is not similarly frozen — it is simply gone, along with the
// rest of that rule, per the paragraph above. Recovering the config from
// HostConfigPrevPath, when one exists, keeps the names either way.
//
// Both the current and the pre-allow-rules renderings are accepted, since an
// upgraded host still has the old artifact on disk until its first mutation.
func Parse(content string) (*Table, error) {
	if !strings.Contains(content, "table "+TableName+" {") {
		return nil, errorx.IllegalFormat.New("not a recognised inet weaver-host-firewall ruleset")
	}

	t := NewTable()

	// Merge each family's set back into the single mixed list the rule holds.
	// Render re-splits by family, so the round-trip is the identity regardless of
	// the mixed-list order (pinned by TestRoundTrip).
	t.Mgmt.CIDRs = parseSetElements(content, reMgmtSet, reMgmtSet6)
	t.Blocked.CIDRs = parseSetElements(content, reBlockedSet, reBlockedSet6)
	t.InCluster.CIDRs = parseSetElements(content, reInClusterSet, reInClusterSet6)

	// A port set declared but carrying no elements means "no ports", which is
	// distinct from a document that predates the set entirely. So the presence of
	// the declaration decides whether to read the element list, and the element
	// list — empty or not — is then authoritative.
	if reMgmtPortDecl.MatchString(content) {
		t.Mgmt.Ports = parseSetElements(content, reMgmtPortSet)
	} else if m := reLegacySSHPort.FindStringSubmatch(content); m != nil {
		// Pre-allow-rules artifact: the management port was a rule literal rather
		// than a set.
		t.Mgmt.Ports = []string{m[1]}
	}
	if reInClusterPortDecl.MatchString(content) {
		t.InCluster.Ports = parseSetElements(content, reInClusterPortSet)
	}

	// Pre-allow-rules artifact: the pod CIDRs were rule literals rather than a
	// set. Only consulted when the set-based form found nothing, so a current
	// document is never second-guessed.
	if len(t.InCluster.CIDRs) == 0 {
		for _, re := range []*regexp.Regexp{reLegacyPodCIDR, reLegacyPodCIDR6} {
			if m := re.FindStringSubmatch(content); m != nil {
				t.InCluster.CIDRs = append(t.InCluster.CIDRs, m[1])
			}
		}
	}

	return t, nil
}

var (
	reMgmtSet           = regexp.MustCompile(`set mgmt_addrs \{[^}]*elements = \{ ([^}]*) \}`)
	reMgmtSet6          = regexp.MustCompile(`set mgmt_addrs6 \{[^}]*elements = \{ ([^}]*) \}`)
	reMgmtPortSet       = regexp.MustCompile(`set mgmt_ports \{[^}]*elements = \{ ([^}]*) \}`)
	reMgmtPortDecl      = regexp.MustCompile(`set mgmt_ports \{`)
	reInClusterPortDecl = regexp.MustCompile(`set in_cluster_ports \{`)
	reBlockedSet        = regexp.MustCompile(`set blocked_addrs \{[^}]*elements = \{ ([^}]*) \}`)
	reBlockedSet6       = regexp.MustCompile(`set blocked_addrs6 \{[^}]*elements = \{ ([^}]*) \}`)
	reInClusterSet      = regexp.MustCompile(`set in_cluster_addrs \{[^}]*elements = \{ ([^}]*) \}`)
	reInClusterSet6     = regexp.MustCompile(`set in_cluster_addrs6 \{[^}]*elements = \{ ([^}]*) \}`)
	reInClusterPortSet  = regexp.MustCompile(`set in_cluster_ports \{[^}]*elements = \{ ([^}]*) \}`)

	// The legacy patterns match the pre-allow-rules rendering, where the
	// management port and the pod CIDRs were rule literals. The address patterns
	// exclude a leading `@` so they cannot match the current set-based rule.
	reLegacySSHPort  = regexp.MustCompile(`ip saddr @mgmt_addrs tcp dport (\d+) accept`)
	reLegacyPodCIDR  = regexp.MustCompile(`ip saddr ([^@\s]\S*) tcp dport @in_cluster_ports accept`)
	reLegacyPodCIDR6 = regexp.MustCompile(`ip6 saddr ([^@\s]\S*) tcp dport @in_cluster_ports accept`)
)

// parseSetElements returns the merged element lists of the named sets, skipping
// any that are absent or declared without an `elements` clause.
func parseSetElements(content string, res ...*regexp.Regexp) []string {
	var out []string
	for _, re := range res {
		m := re.FindStringSubmatch(content)
		if m == nil {
			continue
		}
		out = append(out, splitElements(m[1])...)
	}
	return out
}

func splitElements(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}
