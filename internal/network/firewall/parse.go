// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/joomcode/errorx"
)

// Parse reconstructs a Table from the on-disk network-host.nft artifact. It
// understands only the exact format this package renders (see the embedded
// template) — it is not a general nft parser. A render→parse→render round-trip
// is the identity, which is pinned by TestRoundTrip. Element verbs (add/remove/
// set) use this to load prior state so they don't need the full flag set re-spec.
func Parse(content string) (*Table, error) {
	t := &Table{SSHPort: DefaultSSHPort}

	if !strings.Contains(content, "table "+TableName+" {") {
		return nil, errorx.IllegalFormat.New("not a recognised inet host ruleset")
	}

	if cidrs, ok := parseElements(content, reMgmtSet); ok {
		t.MgmtCIDRs = splitElements(cidrs)
	}
	if cidrs, ok := parseElements(content, reBlockedSet); ok {
		t.BlockedCIDRs = splitElements(cidrs)
	}
	if ports, ok := parseElements(content, rePortSet); ok {
		for _, p := range splitElements(ports) {
			n, err := strconv.Atoi(p)
			if err != nil {
				return nil, errorx.IllegalFormat.Wrap(err, "invalid in-cluster port %q in %s", p, HostNftPath)
			}
			t.InClusterPorts = append(t.InClusterPorts, n)
		}
	}

	if m := reSSHPort.FindStringSubmatch(content); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, errorx.IllegalFormat.Wrap(err, "invalid ssh port %q in %s", m[1], HostNftPath)
		}
		t.SSHPort = n
	}
	if m := rePodCIDR.FindStringSubmatch(content); m != nil {
		t.PodCIDR = m[1]
	}

	return t, nil
}

var (
	reMgmtSet    = regexp.MustCompile(`set mgmt_addrs \{[^}]*elements = \{ ([^}]*) \}`)
	reBlockedSet = regexp.MustCompile(`set blocked_addrs \{[^}]*elements = \{ ([^}]*) \}`)
	rePortSet    = regexp.MustCompile(`set in_cluster_ports \{[^}]*elements = \{ ([^}]*) \}`)
	reSSHPort    = regexp.MustCompile(`ip saddr @mgmt_addrs tcp dport (\d+) accept`)
	rePodCIDR    = regexp.MustCompile(`ip saddr (\S+) tcp dport @in_cluster_ports accept`)
)

func parseElements(content string, re *regexp.Regexp) (string, bool) {
	m := re.FindStringSubmatch(content)
	if m == nil {
		return "", false
	}
	return strings.TrimSpace(m[1]), true
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
