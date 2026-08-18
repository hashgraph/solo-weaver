// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"sort"
	"strconv"

	"github.com/joomcode/errorx"
)

// Default flag values.
const (
	DefaultSSHPort = 22
)

// DefaultInClusterPorts is the "stack set" of host-service ports opened to the
// in-cluster (pod) CIDR by default: the kube-apiserver (6443), the Cilium
// cluster-mesh / health port (4244), the kubelet read-only/metrics port
// (10250), and the MetalLB metrics/memberlist port (7472). Operators override
// with --in-cluster-ports.
var DefaultInClusterPorts = []int{6443, 4244, 7472, 10250}

// Table is the in-memory model of the `inet weaver-host-firewall` nftables table. It is the
// single source of truth that the kernel apply (via `nft -f`), the on-disk nft
// artifact, and the persisted YAML config are all rendered from, so no two of
// them can diverge.
//
// The three reserved blocks are separate fields rather than entries in Allow
// because each renders into a position an allow rule cannot reach: Mgmt also
// feeds the ICMP chains, Blocked renders as a drop on three hooks, and
// InCluster's address list is auto-detected rather than authored. Every other
// rule is uniform, so the CLI addresses all four kinds by name through
// Table.Rule.
type Table struct {
	// Mgmt is the management allowlist (sets @mgmt_addrs / @mgmt_addrs6) and the
	// ports reachable from it (@mgmt_ports). Under the input chain's default
	// drop this is the only path to host-local administrative access, so an
	// empty address list locks the operator out of new connections.
	Mgmt Rule
	// Blocked is the operator-curated deny list (sets @blocked_addrs /
	// @blocked_addrs6). It is purely operator-managed for its whole lifecycle —
	// nothing in this package or the daemon ever writes to it. This is
	// deliberately distinct from the BN workload plane's `bn-restricted` set
	// (`inet weaver-workload-policy`), which the traffic-shaper daemon
	// reconciles from the block node's statusz "restricted" category; an
	// operator block list needs a home the daemon never overwrites.
	//
	// A blocked CIDR means "blocked on this node", not "blocked from the host's
	// own services": it is dropped on prerouting (which covers pod-bound
	// forwarded traffic and runs ahead of conntrack), again on input, and as a
	// destination on output — because blocking a peer inbound does not stop this
	// host from dialing it, and the replies to a host-initiated connection are
	// admitted by the input chain's established accept.
	Blocked Rule
	// InCluster admits host-service ports (@in_cluster_ports) from the pod CIDR
	// (@in_cluster_addrs / @in_cluster_addrs6). Per design there is deliberately
	// no rule here for block-node service ports: that traffic is forwarded
	// rather than delivered locally, so an input rule for it would never match.
	// It lives in `network policy --ports` instead.
	//
	// An empty address list renders no in-cluster rule at all, which is how an
	// operator disables the block without deleting it.
	InCluster Rule
	// Allow holds the operator-authored rules, each a source list x port list x
	// protocol accept. Order within the input chains is by name, so a render is
	// stable across CLI invocations; evaluation order does not matter because
	// every entry is an accept and none overlap a drop.
	Allow []Rule
}

// NewTable returns a Table populated with the design defaults. Callers override
// fields from CLI flags or a config file before rendering.
func NewTable() *Table {
	return &Table{
		Mgmt:      Rule{Name: RuleMgmt, Ports: []string{strconv.Itoa(DefaultSSHPort)}},
		Blocked:   Rule{Name: RuleBlocked},
		InCluster: Rule{Name: RuleInCluster, Ports: PortStrings(DefaultInClusterPorts)},
	}
}

// PortStrings converts an int port list to the string port specs a Rule holds.
// It is the boundary conversion for callers whose own schema is still int-typed
// — the --in-cluster-ports / --ssh-port flags and models.HostConfig — so the
// int-vs-range mismatch is resolved in exactly one place.
func PortStrings(ports []int) []string {
	out := make([]string, len(ports))
	for i, p := range ports {
		out[i] = strconv.Itoa(p)
	}
	sortPortSpecs(out)
	return out
}

// Rule returns a pointer to the named rule so a caller can mutate it in place.
// It resolves the three reserved names and any allow rule through one lookup,
// which is what lets `network firewall add --name <x>` treat them uniformly.
func (t *Table) Rule(name string) (*Rule, bool) {
	switch name {
	case RuleMgmt:
		return &t.Mgmt, true
	case RuleBlocked:
		return &t.Blocked, true
	case RuleInCluster:
		return &t.InCluster, true
	}
	for i := range t.Allow {
		if t.Allow[i].Name == name {
			return &t.Allow[i], true
		}
	}
	return nil, false
}

// Names returns every rule name in the table, reserved blocks first and allow
// rules sorted, for error messages that list the valid --name values.
func (t *Table) Names() []string {
	out := append([]string(nil), ReservedNames...)
	for _, r := range t.Allow {
		out = append(out, r.Name)
	}
	return out
}

// rules returns every rule in the table in render order, for the checks and
// derivations that treat all four kinds alike.
func (t *Table) rules() []*Rule {
	out := []*Rule{&t.Mgmt, &t.Blocked, &t.InCluster}
	for i := range t.Allow {
		out = append(out, &t.Allow[i])
	}
	return out
}

// UpsertAllow adds or replaces an allow rule, rejecting the reserved names. The
// list is kept sorted by name so the rendered document is independent of the
// order rules were authored in.
func (t *Table) UpsertAllow(r Rule) error {
	if IsReserved(r.Name) {
		return errorx.IllegalArgument.New(
			"%q is a reserved name and cannot be used for an allow rule; configure it under its own %q block instead", r.Name, r.Name)
	}
	if err := r.Validate(); err != nil {
		return err
	}
	for i := range t.Allow {
		if t.Allow[i].Name == r.Name {
			t.Allow[i] = r
			return nil
		}
	}
	t.Allow = append(t.Allow, r)
	t.sortAllow()
	return nil
}

// IncompleteAllowRules returns the names of allow rules that are declared but do
// not yet render anything, in table order. Declaring a rule before populating it
// is supported (`network firewall create-allow-rule`), so this is a warning the
// manager surfaces on apply rather than a validation failure.
func (t *Table) IncompleteAllowRules() []string {
	var out []string
	for i := range t.Allow {
		if t.Allow[i].incomplete() {
			out = append(out, t.Allow[i].Name)
		}
	}
	return out
}

// DeleteRule removes an allow rule. The reserved blocks cannot be deleted —
// they are structural, and deleting mgmt in particular would render a
// default-drop input chain with no way in. Emptying a block's address list is
// the supported way to disable it.
func (t *Table) DeleteRule(name string) error {
	if IsReserved(name) {
		return errorx.IllegalArgument.New(
			"%q cannot be deleted: it is a reserved block. Clear its addresses instead (`network firewall set --name %s --cidrs \"\"`)", name, name)
	}
	for i := range t.Allow {
		if t.Allow[i].Name == name {
			t.Allow = append(t.Allow[:i], t.Allow[i+1:]...)
			return nil
		}
	}
	return errorx.IllegalArgument.New("no rule named %q", name)
}

func (t *Table) sortAllow() {
	sort.Slice(t.Allow, func(i, j int) bool { return t.Allow[i].Name < t.Allow[j].Name })
}

// Validate rejects any table that would be unsafe to render. It is the last
// gate before the renderer: it validates every rule, holds the reserved names
// against the allow list, and checks that no two rules derive the same nft set
// name.
func (t *Table) Validate() error {
	// Reserved-block names are structural. A caller that built the table by hand
	// (rather than through NewTable) could leave them empty, which would render
	// a set named "_addrs"; fix them up rather than failing, since the field a
	// value sits in is what identifies the block.
	t.Mgmt.Name = RuleMgmt
	t.Blocked.Name = RuleBlocked
	t.InCluster.Name = RuleInCluster

	for _, r := range t.rules() {
		if err := r.Validate(); err != nil {
			return err
		}
	}

	seenName := make(map[string]struct{}, len(t.Allow))
	for _, r := range t.Allow {
		if IsReserved(r.Name) {
			return errorx.IllegalArgument.New(
				"%q is a reserved name and cannot be used for an allow rule; configure it under its own %q block instead", r.Name, r.Name)
		}
		if _, dup := seenName[r.Name]; dup {
			return errorx.IllegalArgument.New("duplicate allow rule %q", r.Name)
		}
		seenName[r.Name] = struct{}{}
	}

	return t.checkSetNameCollisions()
}

// checkSetNameCollisions rejects a table where two rules derive the same nft set
// name. The derivations append suffixes, so distinct rule names can still
// collide — an allow rule named "mgmt_addrs" would claim the mgmt block's
// address set, and one named "k8s6" would claim the v6 set of a rule named
// "k8s". nft would accept the duplicate declaration silently and merge the two
// rules' membership, so this has to be caught here.
func (t *Table) checkSetNameCollisions() error {
	owner := make(map[string]string)
	claim := func(setName, ruleName string) error {
		if prev, ok := owner[setName]; ok {
			return errorx.IllegalArgument.New(
				"rules %q and %q both derive the nft set name %q; rename one of them", prev, ruleName, setName)
		}
		owner[setName] = ruleName
		return nil
	}
	for _, r := range t.rules() {
		for _, setName := range []string{addrSetName(r.Name), v6SetName(r.Name), portsSetName(r.Name)} {
			if err := claim(setName, r.Name); err != nil {
				return err
			}
		}
	}
	return nil
}
