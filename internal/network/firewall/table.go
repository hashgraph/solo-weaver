// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"net"
	"sort"
	"strconv"

	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
)

// Default flag values per design §8.4.1.
const (
	DefaultSSHPort = 22
)

// DefaultInClusterPorts is the "stack set" of host-service ports opened to the
// in-cluster (pod) CIDR by default: the kube-apiserver (6443), the Cilium
// cluster-mesh / health port (4244), the kubelet read-only/metrics port
// (10250), and the MetalLB metrics/memberlist port (7472). Operators override
// with --in-cluster-ports.
var DefaultInClusterPorts = []int{6443, 4244, 7472, 10250}

// Table is the in-memory model of the `inet host` nftables table. It is the
// single source of truth that both the kernel apply (via `nft -f`) and the
// on-disk artifact are rendered from, so the two can never diverge.
type Table struct {
	// MgmtCIDRs is the management/SSH allowlist (set @mgmt_addrs).
	MgmtCIDRs []string
	// InClusterPorts are host-service ports reachable from PodCIDR (set
	// @in_cluster_ports). Per design there is deliberately no --service-ports:
	// BN ports live only in `network policy --ports`.
	InClusterPorts []int
	// SSHPort is the TCP port accepted from @mgmt_addrs for management access.
	SSHPort int
	// PodCIDR is the source range allowed to reach @in_cluster_ports. Empty
	// means no in-cluster port rule is rendered.
	PodCIDR string
}

// NewTable returns a Table populated with the design defaults. Callers override
// fields from CLI flags before rendering.
func NewTable() *Table {
	ports := append([]int(nil), DefaultInClusterPorts...)
	sort.Ints(ports)
	return &Table{
		MgmtCIDRs:      nil,
		InClusterPorts: ports,
		SSHPort:        DefaultSSHPort,
	}
}

// Validate rejects any field that would be unsafe to render into the nft
// ruleset. It is the last gate before the renderer; every untrusted value
// (CIDRs, ports) is checked through pkg/sanity so a malformed token can never
// break the atomic transaction or smuggle in nft syntax.
func (t *Table) Validate() error {
	for _, c := range t.MgmtCIDRs {
		if err := sanity.ValidateCIDR(c); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid --mgmt-cidr %q", c)
		}
		if ip, _, _ := net.ParseCIDR(c); ip.To4() == nil {
			return errorx.IllegalArgument.New(
				"invalid --mgmt-cidr %q: IPv6 CIDRs are not yet supported; the inet host table uses ipv4_addr sets", c)
		}
	}

	if t.PodCIDR != "" {
		if err := sanity.ValidateCIDR(t.PodCIDR); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid --pod-cidr %q", t.PodCIDR)
		}
		if ip, _, _ := net.ParseCIDR(t.PodCIDR); ip.To4() == nil {
			return errorx.IllegalArgument.New(
				"invalid --pod-cidr %q: IPv6 CIDRs are not yet supported; the inet host table uses ipv4_addr sets", t.PodCIDR)
		}
	}

	for _, p := range t.InClusterPorts {
		if err := sanity.ValidatePort(strconv.Itoa(p)); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid --in-cluster-port %d", p)
		}
	}

	if err := sanity.ValidatePort(strconv.Itoa(t.SSHPort)); err != nil {
		return errorx.IllegalArgument.Wrap(err, "invalid --ssh-port %d", t.SSHPort)
	}

	return nil
}

// AddMgmtCIDR adds a single CIDR to the management allowlist (idempotent).
func (t *Table) AddMgmtCIDR(cidr string) error {
	if err := sanity.ValidateCIDR(cidr); err != nil {
		return errorx.IllegalArgument.Wrap(err, "invalid --mgmt-cidr %q", cidr)
	}
	if sanity.Contains(cidr, t.MgmtCIDRs) {
		return nil
	}
	t.MgmtCIDRs = append(t.MgmtCIDRs, cidr)
	sort.Strings(t.MgmtCIDRs)
	return nil
}

// RemoveMgmtCIDR removes a single CIDR from the management allowlist
// (idempotent; removing an absent CIDR is a no-op).
func (t *Table) RemoveMgmtCIDR(cidr string) {
	out := t.MgmtCIDRs[:0]
	for _, c := range t.MgmtCIDRs {
		if c != cidr {
			out = append(out, c)
		}
	}
	t.MgmtCIDRs = out
}

// SetMgmtCIDRs atomically replaces the full management allowlist.
func (t *Table) SetMgmtCIDRs(cidrs []string) error {
	for _, c := range cidrs {
		if err := sanity.ValidateCIDR(c); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid --mgmt-cidr %q", c)
		}
	}
	dedup := dedupeStrings(cidrs)
	sort.Strings(dedup)
	t.MgmtCIDRs = dedup
	return nil
}

// AddPort adds a single in-cluster host-service port (idempotent).
func (t *Table) AddPort(port int) error {
	if err := sanity.ValidatePort(strconv.Itoa(port)); err != nil {
		return errorx.IllegalArgument.Wrap(err, "invalid --in-cluster-port %d", port)
	}
	for _, p := range t.InClusterPorts {
		if p == port {
			return nil
		}
	}
	t.InClusterPorts = append(t.InClusterPorts, port)
	sort.Ints(t.InClusterPorts)
	return nil
}

// RemovePort removes a single in-cluster host-service port (idempotent).
func (t *Table) RemovePort(port int) {
	out := t.InClusterPorts[:0]
	for _, p := range t.InClusterPorts {
		if p != port {
			out = append(out, p)
		}
	}
	t.InClusterPorts = out
}

// SetPorts atomically replaces the full in-cluster host-service port list.
func (t *Table) SetPorts(ports []int) error {
	for _, p := range ports {
		if err := sanity.ValidatePort(strconv.Itoa(p)); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid --in-cluster-port %d", p)
		}
	}
	dedup := dedupeInts(ports)
	sort.Ints(dedup)
	t.InClusterPorts = dedup
	return nil
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func dedupeInts(in []int) []int {
	seen := make(map[int]struct{}, len(in))
	out := make([]int, 0, len(in))
	for _, n := range in {
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}
