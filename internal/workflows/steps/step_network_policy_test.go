// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"sort"
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/network/policy"
)

// TestCanonicalBNPolicies_Names pins the fixed BN static-plane policy set so a
// change to the design list is a deliberate, reviewed edit.
func TestCanonicalBNPolicies_Names(t *testing.T) {
	want := []string{
		"bn-publisher", "bn-subscriber-in", "bn-partner-out", "bn-public-out",
		"bn-status-in", "bn-status-out", "bn-mgmt-in", "bn-mgmt-out",
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

	seen := map[string]string{} // (direction|ports) → policy name, for specific stamps
	for _, c := range canonicalBNPolicies {
		p := c.toPolicy()
		cidrs := initialCIDRs(c, mgmt)
		if err := p.Validate(cidrs); err != nil {
			t.Fatalf("policy %q failed validation: %v", c.name, err)
		}

		// A "specific" stamp policy renders an IP-set clause (not deny, not
		// --from-entity world). Two such policies sharing a (direction, ports)
		// group would have ambiguous classification.
		if p.Action != policy.ActionStamp || p.FromEntityWorld {
			continue
		}
		ports := append([]string(nil), p.Ports...)
		sort.Strings(ports)
		key := string(p.Direction) + "|" + strings.Join(ports, ",")
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
