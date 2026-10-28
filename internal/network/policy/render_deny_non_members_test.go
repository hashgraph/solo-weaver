// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// publisherGuard is the shape `block node install` lays down for bn-publisher.
func publisherGuard() *Policy {
	return &Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Direction: DirectionIngress,
		Ports: []string{"40840"}, DenyNonMembers: true}
}

// subscriberFallthrough accepts any source on ports — the single-port layout,
// where the publish port is also the subscriber port.
func subscriberFallthrough(ports ...string) *Policy {
	return &Policy{Name: "bn-subscriber-in", Action: ActionStamp, Stamp: "reserve-ingress", Direction: DirectionIngress,
		FromEntityWorld: true, Ports: ports}
}

func TestRender_DenyNonMembersDropsOutsidersInBothFamilies(t *testing.T) {
	doc, err := Render([]*Policy{publisherGuard()}, nil, "10.4.0.0/24", "2001:db8:c0de::/64")
	require.NoError(t, err)

	require.Contains(t, doc,
		"\t\tip daddr 10.4.0.0/24 ip saddr != @bn-publisher tcp dport @bn-publisher_ports ct direction original drop")
	require.Contains(t, doc,
		"\t\tip6 daddr 2001:db8:c0de::/64 ip6 saddr != @bn-publisher6 tcp dport @bn-publisher_ports ct direction original drop")
	require.Contains(t, doc,
		"\t\tip daddr 10.4.0.0/24 ip saddr @bn-publisher tcp dport @bn-publisher_ports counter meta priority set 0x10010 accept comment \"weaver:class=publisher\"",
		"members are still classified into the publisher class")
}

func TestRender_DenyNonMembersSkipsFamilyWithoutPodCIDR(t *testing.T) {
	doc, err := Render([]*Policy{publisherGuard()}, nil, "10.4.0.0/24")
	require.NoError(t, err)

	require.Contains(t, doc, "ip saddr != @bn-publisher ")
	require.NotContains(t, doc, "ip6 saddr != @bn-publisher6")
}

// On a single-port block node the publish port is also in the subscriber
// fallthrough's port set. The guard must run first, or the fallthrough accepts
// the non-member before the guard is reached.
func TestRender_DenyNonMembersPrecedesFallthroughOnSharedPort(t *testing.T) {
	doc, err := Render([]*Policy{publisherGuard(), subscriberFallthrough("40840")}, nil, "10.4.0.0/24")
	require.NoError(t, err)

	guard := strings.Index(doc, "ip saddr != @bn-publisher tcp dport @bn-publisher_ports ct direction original drop")
	fallthr := strings.Index(doc, "tcp dport @bn-subscriber-in_ports counter meta priority set")
	require.NotEqual(t, -1, guard)
	require.NotEqual(t, -1, fallthr)
	require.Less(t, guard, fallthr, "the guard must be evaluated before any accept that shares its port")
}

// The listener port sits inside the ephemeral range, so the drop must not match
// the reply leg of an unrelated connection that drew it as its source port.
func TestRender_DenyNonMembersMatchesRequestLegOnly(t *testing.T) {
	doc, err := Render([]*Policy{publisherGuard()}, nil, "10.4.0.0/24", "2001:db8:c0de::/64")
	require.NoError(t, err)

	var drops int
	for _, line := range strings.Split(doc, "\n") {
		if !strings.Contains(line, "!= @bn-publisher") {
			continue
		}
		drops++
		require.Contains(t, line, "ct direction original")
		require.Contains(t, line, "tcp dport", "a sport match would drop the outbound leg of an ephemeral-port collision")
	}
	require.Equal(t, 2, drops, "one request-leg drop per family with a pod CIDR")
}

func TestRender_StampWithoutDenyNonMembersRendersNoGuard(t *testing.T) {
	p := publisherGuard()
	p.DenyNonMembers = false
	doc, err := Render([]*Policy{p}, nil, "10.4.0.0/24")
	require.NoError(t, err)

	require.NotContains(t, doc, "!= @bn-publisher")
}
