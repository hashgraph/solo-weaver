// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"sort"
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/network/policy"
)

// testHealthPort is a representative resolved health port for toPolicy in tests.
const testHealthPort = "40983"

// TestCanonicalBNPolicies_Names pins the fixed BN static-plane policy set so a
// change to the design list is a deliberate, reviewed edit. bn-status-in/out are
// folded into the public port union (server-status is public to everyone), so
// they are no longer in the canonical set.
func TestCanonicalBNPolicies_Names(t *testing.T) {
	want := []string{
		"bn-publisher", "bn-subscriber-in", "bn-partner-out", "bn-public-out",
		"bn-mgmt-in", "bn-mgmt-out",
		"bn-restricted", "bn-backfill",
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
	mgmt := []string{"10.0.0.0/8"}

	seen := map[string]string{} // group key → policy name, for specific stamps
	for _, c := range canonicalBNPolicies {
		p := c.toPolicy(testHealthPort)
		cidrs := initialCIDRs(c, mgmt)
		if err := p.Validate(cidrs); err != nil {
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

// TestInitialCIDRs checks curated-set membership routing: mgmt sets get the
// management allowlist, everything else (including bn-restricted) starts empty
// (populated by the daemon poll loop).
func TestInitialCIDRs(t *testing.T) {
	mgmt := []string{"10.0.0.0/8"}
	byName := map[string]canonicalPolicy{}
	for _, c := range canonicalBNPolicies {
		byName[c.name] = c
	}

	if got := initialCIDRs(byName["bn-mgmt-in"], mgmt); len(got) != 1 || got[0] != "10.0.0.0/8" {
		t.Errorf("bn-mgmt-in cidrs: got %v, want mgmt list", got)
	}
	if got := initialCIDRs(byName["bn-restricted"], mgmt); got != nil {
		t.Errorf("bn-restricted cidrs: got %v, want nil (daemon-reconciled, not operator-curated)", got)
	}
	if got := initialCIDRs(byName["bn-publisher"], mgmt); got != nil {
		t.Errorf("bn-publisher cidrs: got %v, want nil (daemon-reconciled)", got)
	}
}

// TestCanonicalBNPolicies_ManagedPortsAndHealth pins which sets have
// daemon-reconciled listener ports (no static literals) versus the bn-mgmt sets,
// whose ports are seeded from the resolved health port.
func TestCanonicalBNPolicies_ManagedPortsAndHealth(t *testing.T) {
	byName := map[string]canonicalPolicy{}
	for _, c := range canonicalBNPolicies {
		byName[c.name] = c
	}

	for _, n := range []string{"bn-publisher", "bn-subscriber-in", "bn-partner-out", "bn-public-out"} {
		p := byName[n].toPolicy(testHealthPort)
		if !p.ManagedPorts {
			t.Errorf("%s: want ManagedPorts (statusz-derived)", n)
		}
		if len(p.Ports) != 0 {
			t.Errorf("%s: a managed-ports policy must carry no static port literals, got %v", n, p.Ports)
		}
	}

	for _, n := range []string{"bn-mgmt-in", "bn-mgmt-out"} {
		p := byName[n].toPolicy(testHealthPort)
		if p.ManagedPorts {
			t.Errorf("%s: bn-mgmt ports come from Helm, not statusz", n)
		}
		if len(p.Ports) != 1 || p.Ports[0] != testHealthPort {
			t.Errorf("%s: want resolved health port %q, got %v", n, testHealthPort, p.Ports)
		}
	}
}
