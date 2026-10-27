// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// legacyPublisher is bn-publisher as a release before deny-non-members created
// it: statusz-managed ports and no guard.
func legacyPublisher() *Policy {
	return &Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", ManagedPorts: true, CreatedAt: fixedTime()}
}

func TestRedefine_ConvergesDefinitionAndKeepsLiveMembership(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, regDir := newTestManager(t, r)
	ctx := context.Background()

	_, err := m.Create(ctx, legacyPublisher(), []string{"10.1.0.1/32", "2001:db8::1/128"}, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)

	changed, err := m.Redefine(ctx, publisherGuard(), []string{"10.4.0.0/24"})
	require.NoError(t, err)
	require.True(t, changed)

	require.Equal(t, []string{"10.1.0.1"}, r.elements["bn-publisher"],
		"publishers admitted before the upgrade must still be admitted after it")
	require.Equal(t, []string{"2001:db8::1"}, r.elements["bn-publisher6"])

	got, err := readEntry(regDir, "bn-publisher")
	require.NoError(t, err)
	require.True(t, got.DenyNonMembers)
	require.False(t, got.ManagedPorts)
	require.Equal(t, []string{"40840"}, got.Ports)
	require.Equal(t, fixedTime(), got.CreatedAt, "the tier tiebreaker survives a redefinition")

	doc, err := os.ReadFile(nftPath)
	require.NoError(t, err)
	require.Contains(t, string(doc), "set bn-publisher_ports { type inet_service; elements = { 40840 }; }")
	require.Contains(t, string(doc), "ip saddr != @bn-publisher tcp dport @bn-publisher_ports ct direction original drop")
	require.Contains(t, string(doc), "set bn-publisher { type ipv4_addr; flags interval; elements = { 10.1.0.1 }; }",
		"the boot artifact carries the kept membership too")
}

func TestRedefine_SameDefinitionIsNoOp(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	ctx := context.Background()

	_, err := m.Create(ctx, publisherGuard(), []string{"10.1.0.1/32"}, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)
	applies := r.applyCount

	changed, err := m.Redefine(ctx, publisherGuard(), []string{"10.4.0.0/24"})
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, applies, r.applyCount, "an unchanged definition must not re-render the table")
}

func TestRedefine_MissingPolicyIsAnError(t *testing.T) {
	m, _, _ := newTestManager(t, newFakeRunner())

	_, err := m.Redefine(context.Background(), publisherGuard(), []string{"10.4.0.0/24"})
	require.ErrorContains(t, err, "not found")
}

func TestRedefine_KeepsSiblingMembership(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	ctx := context.Background()

	_, err := m.Create(ctx, legacyPublisher(), nil, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)
	seedDenyPolicy(t, m, "bn-restricted", []string{"10.99.0.0/16"})

	_, err = m.Redefine(ctx, publisherGuard(), []string{"10.4.0.0/24"})
	require.NoError(t, err)
	require.Equal(t, []string{"10.99.0.0/16"}, r.elements["bn-restricted"])
}

func TestMembers_ReturnsBothFamilies(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	ctx := context.Background()

	_, err := m.Create(ctx, publisherGuard(), nil, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)

	got, err := m.Members(ctx, "bn-publisher")
	require.NoError(t, err)
	require.Empty(t, got)

	require.NoError(t, m.Add(ctx, "bn-publisher", []string{"10.1.0.1/32", "2001:db8::1/128"}))
	got, err = m.Members(ctx, "bn-publisher")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"10.1.0.1/32", "2001:db8::1/128"}, got)
}

func TestSameDefinition_IgnoresCreatedAtAndDirectionAndPortOrder(t *testing.T) {
	a := publisherGuard()
	b := publisherGuard()
	b.CreatedAt = fixedTime()
	b.Direction = ""
	require.True(t, a.SameDefinition(b))

	a.Ports, b.Ports = []string{"40840", "40841"}, []string{"40841", "40840"}
	require.True(t, a.SameDefinition(b))

	b.DenyNonMembers = false
	require.False(t, a.SameDefinition(b))
}
