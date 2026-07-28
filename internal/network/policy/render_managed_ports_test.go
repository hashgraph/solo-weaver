// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRender_ManagedPortsSetDeclaredEmptyWithDportClause(t *testing.T) {
	// A managed-ports policy: its <name>_ports set is declared but empty (the
	// daemon fills it from statusz), yet the classification rule still references
	// it so the port match takes effect once populated.
	managed := &Policy{
		Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher",
		Direction: DirectionIngress, ManagedPorts: true,
	}
	doc, err := Render([]*Policy{managed}, "10.4.0.0/24")
	require.NoError(t, err)

	require.Contains(t, doc, "set bn-publisher_ports { type inet_service; }",
		"managed ports set is declared empty (no inline elements)")
	require.NotContains(t, doc, "bn-publisher_ports { type inet_service; elements",
		"managed ports set must not carry inline elements")
	require.Contains(t, doc, "tcp dport @bn-publisher_ports",
		"the classification rule references the ports set even while it is empty")
}

func TestRender_StaticPortsSetKeepsInlineElements(t *testing.T) {
	static := &Policy{
		Name: "bn-mgmt-in", Action: ActionStamp, Stamp: "reserve-ingress",
		Direction: DirectionIngress, Ports: []string{"40983"},
	}
	doc, err := Render([]*Policy{static}, "10.4.0.0/24")
	require.NoError(t, err)

	require.Contains(t, doc, "set bn-mgmt-in_ports { type inet_service; elements = { 40983 }; }")
	require.Contains(t, doc, "tcp dport @bn-mgmt-in_ports")
}
