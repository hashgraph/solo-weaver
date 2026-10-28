// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"sort"
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/network/policy"
)

// testHealthPort and testPublisherPort are representative resolved ports for
// toPolicy in tests.
const (
	testHealthPort    = "40983"
	testPublisherPort = "40840"
)

var testPorts = bnPorts{health: testHealthPort, publisher: testPublisherPort}

// TestCanonicalBNPolicies_Names pins the fixed BN static-plane policy set so a
// change to the design list is a deliberate, reviewed edit. bn-status-in/out are
// folded into the public port union (server-status is public to everyone), and
// bn-mgmt-in/out are replaced by the bn-health drop, so none of the four are in
// the canonical set.
func TestCanonicalBNPolicies_Names(t *testing.T) {
	want := []string{
		"bn-publisher", "bn-subscriber-in", "bn-partner-out", "bn-public-out",
		"bn-health", "bn-restricted", "bn-backfill",
	}
	if len(canonicalBNPolicies) != len(want) {
		t.Fatalf("policy count: got %d, want %d", len(canonicalBNPolicies), len(want))
	}
	for i, c := range canonicalBNPolicies {
		if c.name != want[i] {
			t.Errorf("policy[%d]: got %q, want %q", i, c.name, want[i])
		}
	}
}

// TestCanonicalBNPolicies_Valid verifies every canonical policy validates (the
// class names resolve, flag combinations are legal) and that no two "specific"
// stamp policies share the same (direction, ports) group — the overlap the
// policy manager would reject at create time.
func TestCanonicalBNPolicies_Valid(t *testing.T) {
	seen := map[string]string{} // group key → policy name, for specific stamps
	for _, c := range canonicalBNPolicies {
		p := c.toPolicy(testPorts)
		// No membership is seeded at install: every set starts empty.
		if err := p.Validate(nil); err != nil {
			t.Fatalf("policy %q failed validation: %v", c.name, err)
		}

		// A "specific" stamp policy renders an IP-set clause (not deny, not
		// --from-entity world). Two such policies sharing a group would have
		// ambiguous classification.
		if p.Action != policy.ActionStamp || p.FromEntityWorld {
			continue
		}
		// Mirror policy.groupKey: managed-ports policies are keyed per-name (their
		// real listener ports are reconciled from statusz and distinct by design),
		// so they never statically overlap; static-ports policies key on
		// (direction, ports).
		var key string
		if p.ManagedPorts {
			key = string(p.Direction) + "|managed:" + p.Name
		} else {
			ports := append([]string(nil), p.Ports...)
			sort.Strings(ports)
			key = string(p.Direction) + "|" + strings.Join(ports, ",")
		}
		if other, dup := seen[key]; dup {
			t.Errorf("policies %q and %q overlap on group %q", c.name, other, key)
		}
		seen[key] = c.name
	}
}

// TestCanonicalBNPolicies_HealthIsADropFromEveryone pins the shape of the
// bn-health entry: a deny, matched on the resolved health port alone, with no
// membership set to narrow it. Its consumers (kubelet, the provisioner) generate
// their traffic on the node, so they never reach the forward hook this table
// registers on and need no allowlist.
func TestCanonicalBNPolicies_HealthIsADropFromEveryone(t *testing.T) {
	var health canonicalPolicy
	var found bool
	for _, c := range canonicalBNPolicies {
		if c.name == "bn-health" {
			health, found = c, true
		}
	}
	// Without this, a removed or renamed bn-health leaves the zero value, and the
	// assertions below fail as "wrong action" rather than "policy is gone".
	if !found {
		t.Fatal("bn-health is not in canonicalBNPolicies")
	}

	p := health.toPolicy(testPorts)
	if p.Action != policy.ActionDeny {
		t.Errorf("bn-health action: got %q, want deny", p.Action)
	}
	if !p.FromEntityWorld {
		t.Error("bn-health must render no membership set: the port is dropped from every source")
	}
	if len(p.Ports) != 1 || p.Ports[0] != testHealthPort {
		t.Errorf("bn-health ports: got %v, want the resolved health port %q", p.Ports, testHealthPort)
	}
}

// TestObsoleteBNPolicies_CoversRemovedMgmtSets pins that a host installed by an
// earlier release has its bn-mgmt-* registry entries and live sets removed rather
// than left orphaned.
func TestObsoleteBNPolicies_CoversRemovedMgmtSets(t *testing.T) {
	for _, name := range []string{"bn-mgmt-in", "bn-mgmt-out"} {
		found := false
		for _, o := range obsoleteBNPolicies {
			if o == name {
				found = true
			}
		}
		if !found {
			t.Errorf("%s is not in obsoleteBNPolicies; an upgrade would leave it behind", name)
		}
		for _, c := range canonicalBNPolicies {
			if c.name == name {
				t.Errorf("%s is both canonical and obsolete", name)
			}
		}
	}
}

// TestCanonicalBNPolicies_PortSources pins which sets have daemon-reconciled
// listener ports (no static literals) versus bn-health and bn-publisher, whose
// ports are seeded from the chart values.
func TestCanonicalBNPolicies_PortSources(t *testing.T) {
	byName := map[string]canonicalPolicy{}
	for _, c := range canonicalBNPolicies {
		byName[c.name] = c
	}

	for _, n := range []string{"bn-subscriber-in", "bn-partner-out", "bn-public-out"} {
		p := byName[n].toPolicy(testPorts)
		if !p.ManagedPorts {
			t.Errorf("%s: want ManagedPorts (statusz-derived)", n)
		}
		if len(p.Ports) != 0 {
			t.Errorf("%s: a managed-ports policy must carry no static port literals, got %v", n, p.Ports)
		}
	}

	for name, want := range map[string]string{"bn-health": testHealthPort, "bn-publisher": testPublisherPort} {
		p := byName[name].toPolicy(testPorts)
		if p.ManagedPorts {
			t.Errorf("%s: the port comes from the chart values, not statusz", name)
		}
		if len(p.Ports) != 1 || p.Ports[0] != want {
			t.Errorf("%s: want resolved port %q, got %v", name, want, p.Ports)
		}
	}
}

// TestCanonicalBNPolicies_PublisherDeniesNonMembers pins that bn-publisher
// guards the publish port: sources outside its membership set are dropped, so
// only publishers the block node knows about can stream blocks.
func TestCanonicalBNPolicies_PublisherDeniesNonMembers(t *testing.T) {
	for _, c := range canonicalBNPolicies {
		p := c.toPolicy(testPorts)
		if c.name == "bn-publisher" {
			if !p.DenyNonMembers {
				t.Error("bn-publisher must deny non-members on the publish port")
			}
			continue
		}
		if p.DenyNonMembers {
			t.Errorf("%s: only bn-publisher denies non-members", c.name)
		}
	}
}
