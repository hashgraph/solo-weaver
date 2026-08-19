// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// managedPortsPolicy is the fixture both managed-ports render tests use: its
// <name>_ports set is daemon-filled from statusz rather than declared in the
// registry.
func managedPortsPolicy() *Policy {
	return &Policy{
		Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher",
		Direction: DirectionIngress, ManagedPorts: true,
	}
}

func TestRender_ManagedPortsSetDeclaredEmptyWhenNoMembership(t *testing.T) {
	// With no membership supplied — the provisioning render, which has no view
	// of live nft state — the set is declared empty, yet the classification rule
	// still references it so the port match takes effect once populated.
	doc, err := Render([]*Policy{managedPortsPolicy()}, nil, "10.4.0.0/24")
	require.NoError(t, err)

	require.Contains(t, doc, "set bn-publisher_ports { type inet_service; }",
		"managed ports set is declared empty when no membership is supplied")
	require.NotContains(t, doc, "bn-publisher_ports { type inet_service; elements",
		"managed ports set must not invent elements the caller did not supply")
	require.Contains(t, doc, "tcp dport @bn-publisher_ports",
		"the classification rule references the ports set even while it is empty")
}

func TestRender_ManagedPortsSetSeededFromMembership(t *testing.T) {
	// The daemon's persist path supplies the live listener ports, which are
	// rendered inline so the boot oneshot replays them ahead of the first poll.
	doc, err := Render([]*Policy{managedPortsPolicy()},
		map[string][]string{"bn-publisher_ports": {"40840", "40841"}}, "10.4.0.0/24")
	require.NoError(t, err)

	require.Contains(t, doc, "set bn-publisher_ports { type inet_service; elements = { 40840, 40841 }; }")
}

func TestRender_StaticPortsSetKeepsInlineElements(t *testing.T) {
	static := &Policy{
		Name: "bn-subscriber-in", Action: ActionStamp, Stamp: "reserve-ingress",
		Direction: DirectionIngress, Ports: []string{"40980"},
	}
	doc, err := Render([]*Policy{static}, nil, "10.4.0.0/24")
	require.NoError(t, err)

	require.Contains(t, doc, "set bn-subscriber-in_ports { type inet_service; elements = { 40980 }; }")
	require.Contains(t, doc, "tcp dport @bn-subscriber-in_ports")
}
