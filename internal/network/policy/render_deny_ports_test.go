// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// healthDeny is the shape `block node install` lays down for bn-health: drop the
// request leg of any connection to one workload listener port, from every source.
func healthDeny() *Policy {
	return &Policy{Name: "bn-health", Action: ActionDeny, FromEntityWorld: true, Ports: []string{"40983"}}
}

func TestRender_PortScopedDenyIsPodScopedInBothFamilies(t *testing.T) {
	doc, err := Render([]*Policy{healthDeny()}, nil, "10.4.0.0/24", "2001:db8:c0de::/64")
	require.NoError(t, err)

	require.Contains(t, doc, "set bn-health_ports { type inet_service; elements = { 40983 }; }",
		"the port list is rendered inline, so it survives a replay of the artifact")
	require.NotContains(t, doc, "set bn-health {",
		"--from-entity world renders no membership set to match on")

	require.Contains(t, doc, "\t\tip daddr 10.4.0.0/24 tcp dport @bn-health_ports ct direction original drop")
	require.Contains(t, doc, "\t\tip6 daddr 2001:db8:c0de::/64 tcp dport @bn-health_ports ct direction original drop")
}

// A listener port sits inside the default ephemeral range (32768-60999), so an
// unrelated connection can draw it as its source port. Matching on `sport` would
// drop that connection's outbound leg, and matching on `dport` without a
// direction qualifier would drop its reply — both silently, since SYN
// retransmits reuse the port. Neither spelling may appear.
func TestRender_PortScopedDenyCannotMatchAnEphemeralSourcePort(t *testing.T) {
	doc, err := Render([]*Policy{healthDeny()}, nil, "10.4.0.0/24", "2001:db8:c0de::/64")
	require.NoError(t, err)

	require.NotContains(t, doc, "tcp sport @bn-health_ports",
		"an egress mirror would drop the outbound leg of a connection that drew the listener port as its ephemeral source port")

	var drops int
	for _, line := range strings.Split(doc, "\n") {
		if !strings.HasSuffix(strings.TrimSpace(line), "drop") || !strings.Contains(line, "@bn-health_ports") {
			continue
		}
		drops++
		require.Contains(t, line, "ct direction original",
			"an unqualified drop also matches the reply leg of an ephemeral-port collision: %s", strings.TrimSpace(line))
	}
	require.Equal(t, 2, drops, "one request-leg drop per family with a pod CIDR")
}

func TestRender_PortScopedDenySkipsFamilyWithoutPodCIDR(t *testing.T) {
	doc, err := Render([]*Policy{healthDeny()}, nil, "10.4.0.0/24")
	require.NoError(t, err)

	require.Contains(t, doc, "\t\tip daddr 10.4.0.0/24 tcp dport @bn-health_ports ct direction original drop")
	require.NotContains(t, doc, "ip6 daddr",
		"a family with no pod CIDR has no pods to protect, so it renders no drop")
	require.Contains(t, doc, "chain forward_ipv6 { }")
}

// A port-scoped deny reads POD_CIDR, so rendering one without any pod CIDR is an
// error rather than a silently empty chain — the membership-only deny below is
// the case that legitimately renders without one.
func TestRender_PortScopedDenyRequiresAPodCIDR(t *testing.T) {
	_, err := Render([]*Policy{healthDeny()}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "pod CIDR is required")

	_, err = Render([]*Policy{{Name: "bn-restricted", Action: ActionDeny}}, nil)
	require.NoError(t, err, "a membership-only deny never references POD_CIDR")
}

// A membership-only deny is unchanged by the port-scoped form: still both
// directions, still not pod-scoped, still no conntrack qualifier.
func TestRender_MembershipOnlyDenyIsUnchanged(t *testing.T) {
	doc, err := Render([]*Policy{{Name: "bn-restricted", Action: ActionDeny}}, nil, "10.4.0.0/24")
	require.NoError(t, err)

	require.Contains(t, doc, "\t\tip saddr @bn-restricted drop")
	require.Contains(t, doc, "\t\tip daddr @bn-restricted drop")
	body := chainBody(t, doc, chainV4)
	require.NotContains(t, body, "ct direction original",
		"the membership deny drops on the set alone; there is no port to collide with")
}

func TestRender_PortScopedDenyKeepsItsMembershipClause(t *testing.T) {
	narrowed := &Policy{Name: "bn-quarantine-http", Action: ActionDeny, Ports: []string{"8080"}}
	doc, err := Render([]*Policy{narrowed}, nil, "10.4.0.0/24")
	require.NoError(t, err)

	require.Contains(t, doc,
		"\t\tip daddr 10.4.0.0/24 ip saddr @bn-quarantine-http tcp dport @bn-quarantine-http_ports ct direction original drop")
	require.NotContains(t, doc, "tcp sport @bn-quarantine-http_ports")
}

// The deny tier runs ahead of every classification accept, so a packet matching
// both a drop and a stamp rule is dropped rather than stamped.
func TestRender_PortScopedDenyPrecedesClassification(t *testing.T) {
	stamp := &Policy{
		Name: "bn-subscriber-in", Action: ActionStamp, Stamp: "reserve-ingress",
		Direction: DirectionIngress, FromEntityWorld: true, Ports: []string{"40980"},
	}
	doc, err := Render([]*Policy{stamp, healthDeny()}, nil, "10.4.0.0/24")
	require.NoError(t, err)

	body := chainBody(t, doc, chainV4)
	dropIdx := strings.Index(body, "@bn-health_ports ct direction original drop")
	stampIdx := strings.Index(body, "tcp dport @bn-subscriber-in_ports meta priority")
	// Guard both lookups: a missing substring indexes to -1, which would satisfy
	// the ordering assertion below and let the test pass with no drop at all.
	require.Positive(t, dropIdx, "the health drop is missing from the chain")
	require.Positive(t, stampIdx, "the classification accept is missing from the chain")
	require.Less(t, dropIdx, stampIdx, "the drop must be evaluated before the classification accept")
}
