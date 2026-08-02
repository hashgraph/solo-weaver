// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// podCIDRArg wraps a single pod CIDR for Manager.Create's []string parameter,
// mapping "" to nil so the empty case still triggers .nft pod-CIDR recovery
// rather than passing a stray empty element.
func podCIDRArg(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

func TestManager_MembershipRoutesByFamily(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")

	ctx := context.Background()

	// A mixed v4/v6 Add routes each family to its own set (@name and @name6).
	require.NoError(t, m.Add(ctx, "bn-publisher", []string{"10.1.0.1/32", "2001:db8::1/128"}))
	require.Equal(t, []string{"10.1.0.1/32"}, r.elements["bn-publisher"])
	require.Equal(t, []string{"2001:db8::1/128"}, r.elements["bn-publisher6"])

	// Set full-replaces each family independently: a v6-only Set clears the v4
	// set (present/absent reconcile semantics per family).
	require.NoError(t, m.Set(ctx, "bn-publisher", []string{"2001:db8::2/128"}))
	require.Empty(t, r.elements["bn-publisher"])
	require.Equal(t, []string{"2001:db8::2/128"}, r.elements["bn-publisher6"])

	// Show surfaces both families' live sets.
	out, err := m.Show(ctx, "bn-publisher")
	require.NoError(t, err)
	require.Contains(t, out, "live set @bn-publisher:")
	require.Contains(t, out, "live set @bn-publisher6:")
}

// seedPolicy creates a policy via Create and verifies no error.
func seedPolicy(t *testing.T, m *Manager, name, stamp string, ports []string, cidrs []string, podCIDR string) {
	t.Helper()
	_, err := m.Create(context.Background(),
		&Policy{Name: name, Action: ActionStamp, Stamp: stamp, Ports: ports},
		cidrs, podCIDRArg(podCIDR), false)
	require.NoError(t, err)
}

func seedDenyPolicy(t *testing.T, m *Manager, name string, cidrs []string) {
	t.Helper()
	_, err := m.Create(context.Background(),
		&Policy{Name: name, Action: ActionDeny},
		cidrs, nil, false)
	require.NoError(t, err)
}

// --- Add ---

func TestAdd_AddsToLiveSet(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")

	require.NoError(t, m.Add(context.Background(), "bn-publisher", []string{"10.1.0.1/32", "10.1.0.2/32"}))
	require.Equal(t, []string{"10.1.0.1/32", "10.1.0.2/32"}, r.elements["bn-publisher"])
}

func TestAdd_AppendsToPriorMembership(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, []string{"10.1.0.1/32"}, "10.4.0.0/24")
	require.Equal(t, []string{"10.1.0.1/32"}, r.elements["bn-publisher"])

	require.NoError(t, m.Add(context.Background(), "bn-publisher", []string{"10.1.0.2/32"}))
	require.Equal(t, []string{"10.1.0.1/32", "10.1.0.2/32"}, r.elements["bn-publisher"])
}

func TestAdd_PolicyNotFound(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)

	err := m.Add(context.Background(), "bn-nonexistent", []string{"10.0.0.1/32"})
	require.ErrorContains(t, err, "not found")
}

func TestAdd_FromEntityWorldPolicyRejected(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-public-out", Action: ActionStamp, Stamp: "public", FromEntityWorld: true, Ports: []string{"40980"}},
		nil, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)

	err = m.Add(context.Background(), "bn-public-out", []string{"10.0.0.1/32"})
	require.ErrorContains(t, err, "no CIDR set")
}

func TestAdd_TableMissingReturnsError(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")

	require.NoError(t, r.Delete(context.Background()))

	err := m.Add(context.Background(), "bn-publisher", []string{"10.1.0.1/32"})
	require.ErrorContains(t, err, "policy table not found")
}

func TestAdd_EmptyCIDRsRejected(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")

	err := m.Add(context.Background(), "bn-publisher", nil)
	require.ErrorContains(t, err, "at least one --cidr")
}

// --- Remove ---

func TestRemove_RemovesFromLiveSet(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"},
		[]string{"10.1.0.1/32", "10.1.0.2/32"}, "10.4.0.0/24")

	require.NoError(t, m.Remove(context.Background(), "bn-publisher", []string{"10.1.0.1/32"}))
	require.Equal(t, []string{"10.1.0.2/32"}, r.elements["bn-publisher"])
}

func TestRemove_PolicyNotFound(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)

	err := m.Remove(context.Background(), "bn-nonexistent", []string{"10.0.0.1/32"})
	require.ErrorContains(t, err, "not found")
}

func TestRemove_TableMissingReturnsError(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")

	require.NoError(t, r.Delete(context.Background()))

	err := m.Remove(context.Background(), "bn-publisher", []string{"10.1.0.1/32"})
	require.ErrorContains(t, err, "policy table not found")
}

// --- Set ---

func TestSet_ReplacesEntireMembership(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"},
		[]string{"10.1.0.1/32", "10.1.0.2/32"}, "10.4.0.0/24")

	require.NoError(t, m.Set(context.Background(), "bn-publisher", []string{"10.5.0.0/24"}))
	require.Equal(t, []string{"10.5.0.0/24"}, r.elements["bn-publisher"],
		"Set replaces all prior membership with exactly the new list")
}

func TestSet_EmptySliceClearsMembership(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"},
		[]string{"10.1.0.1/32"}, "10.4.0.0/24")

	require.NoError(t, m.Set(context.Background(), "bn-publisher", []string{}))
	require.Empty(t, r.elements["bn-publisher"], "Set with empty cidrs clears the set")
}

func TestSet_PolicyNotFound(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)

	err := m.Set(context.Background(), "bn-nonexistent", []string{"10.0.0.1/32"})
	require.ErrorContains(t, err, "not found")
}

func TestSet_TableMissingReturnsError(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")

	require.NoError(t, r.Delete(context.Background()))

	err := m.Set(context.Background(), "bn-publisher", []string{"10.1.0.1/32"})
	require.ErrorContains(t, err, "policy table not found")
}

// --- Show ---

func TestShow_StampPolicy(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"},
		[]string{"10.1.0.1/32"}, "10.4.0.0/24")

	out, err := m.Show(context.Background(), "bn-publisher")
	require.NoError(t, err)
	require.Contains(t, out, "policy: bn-publisher")
	require.Contains(t, out, "action:  stamp")
	require.Contains(t, out, "class:   publisher")
	require.Contains(t, out, "direction: ingress")
	require.Contains(t, out, "ports:   40840")
	require.Contains(t, out, "live set @bn-publisher:")
	require.Contains(t, out, "10.1.0.1/32")
}

func TestShow_Layout_DirectionLeadsAndLiveSetNested(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"},
		[]string{"10.1.0.1/32"}, "10.4.0.0/24")

	out, err := m.Show(context.Background(), "bn-publisher")
	require.NoError(t, err)

	// direction is the first field under the policy header (before action).
	require.Less(t, strings.Index(out, "direction:"), strings.Index(out, "action:"),
		"direction must be listed before action")

	// The live set is nested inside the policy block: its header is indented two
	// spaces (same level as action/class/created) and its members four spaces —
	// not flush-left as a separate top-level section.
	require.Contains(t, out, "  live set @bn-publisher:\n")
	require.Contains(t, out, "    10.1.0.1/32\n")
	require.NotContains(t, out, "\nlive set @", "live set must not be a flush-left top-level section")
}

func TestShow_DenyPolicy(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedDenyPolicy(t, m, "bn-restricted", []string{"10.99.0.0/16"})

	out, err := m.Show(context.Background(), "bn-restricted")
	require.NoError(t, err)
	require.Contains(t, out, "action:  deny")
	require.Contains(t, out, "live set @bn-restricted:")
	require.Contains(t, out, "10.99.0.0/16")
}

func TestShow_FromEntityWorldPolicy(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-public-out", Action: ActionStamp, Stamp: "public", FromEntityWorld: true, Ports: []string{"40980"}},
		nil, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)

	out, err := m.Show(context.Background(), "bn-public-out")
	require.NoError(t, err)
	require.Contains(t, out, "from-entity: world")
	require.Contains(t, out, "live set: none")
}

func TestShow_EmptyLiveSet(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")

	out, err := m.Show(context.Background(), "bn-publisher")
	require.NoError(t, err)
	require.Contains(t, out, "(empty)")
}

func TestShow_PolicyNotFound(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)

	_, err := m.Show(context.Background(), "bn-nonexistent")
	require.ErrorContains(t, err, "not found")
}

// --- Delete ---

func TestDelete_RemovesRegistryAndRerendersChain(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, regDir := newTestManager(t, r)
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"},
		[]string{"10.1.0.1/32"}, "10.4.0.0/24")
	seedDenyPolicy(t, m, "bn-restricted", []string{"10.99.0.0/16"})

	require.NoError(t, m.Delete(context.Background(), "bn-publisher"))

	// Registry file removed.
	require.NoFileExists(t, filepath.Join(regDir, "bn-publisher.json"))
	// Registry for sibling intact.
	require.FileExists(t, filepath.Join(regDir, "bn-restricted.json"))

	// The .nft file no longer references the deleted policy.
	onDisk, err := os.ReadFile(nftPath)
	require.NoError(t, err)
	require.NotContains(t, string(onDisk), "bn-publisher")
	require.Contains(t, string(onDisk), "bn-restricted")
}

func TestDelete_PreservesSiblingMembership(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedDenyPolicy(t, m, "bn-restricted", []string{"10.99.0.0/16"})
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"},
		[]string{"10.1.0.1/32"}, "10.4.0.0/24")

	require.Equal(t, []string{"10.99.0.0/16"}, r.elements["bn-restricted"])
	require.Equal(t, []string{"10.1.0.1/32"}, r.elements["bn-publisher"])

	// Delete bn-publisher: the chain re-render wipes all sets, then restores
	// bn-restricted's snapshot.
	require.NoError(t, m.Delete(context.Background(), "bn-publisher"))

	require.Equal(t, []string{"10.99.0.0/16"}, r.elements["bn-restricted"],
		"sibling live membership must survive the destructive re-render")
	require.Empty(t, r.elements["bn-publisher"], "deleted policy's set no longer present")
}

func TestDelete_LastPolicy_TearsDownTable(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, regDir := newTestManager(t, r)
	seedDenyPolicy(t, m, "bn-restricted", []string{"10.99.0.0/16"})

	require.NoError(t, m.Delete(context.Background(), "bn-restricted"))

	// Deleting the last policy tears the whole table down rather than applying
	// an empty policy-drop chain that would blackhole all forwarded traffic.
	require.False(t, r.exists, "inet weaver table must be deleted after the last policy is removed")
	// The persisted file is removed so the boot oneshot replays nothing.
	require.NoFileExists(t, nftPath)
	// The registry entry is gone.
	require.NoFileExists(t, filepath.Join(regDir, "bn-restricted.json"))
}

func TestDelete_PolicyNotFound(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)

	err := m.Delete(context.Background(), "bn-nonexistent")
	require.ErrorContains(t, err, "not found")
}

func TestDelete_RecoversPodCIDRFromNftFileForRemainingStampPolicies(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)

	// Create a stamp policy (which writes pod CIDR into the .nft) and a deny.
	seedPolicy(t, m, "bn-publisher", "publisher", []string{"40840"}, nil, "10.4.0.0/24")
	seedDenyPolicy(t, m, "bn-restricted", []string{"10.99.0.0/16"})

	// Delete the deny policy — the remaining stamp sibling needs a pod CIDR
	// to re-render, but none is supplied. It must be recovered from the .nft.
	require.NoError(t, m.Delete(context.Background(), "bn-restricted"))

	onDisk, err := os.ReadFile(nftPath)
	require.NoError(t, err)
	require.Contains(t, string(onDisk), "10.4.0.0/24",
		"bn-publisher's rule must still be rendered with the recovered pod CIDR")
	require.NotContains(t, string(onDisk), "bn-restricted")
}

func TestDelete_CompoundSetPolicy(t *testing.T) {
	r := newFakeRunner()
	m, _, regDir := newTestManager(t, r)
	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-backfill", Action: ActionStamp, Stamp: "reserve-egress", ReplyStamp: "backfill-response"},
		[]string{"10.30.5.7:43473"}, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)

	require.NoError(t, m.Delete(context.Background(), "bn-backfill"))
	require.NoFileExists(t, filepath.Join(regDir, "bn-backfill.json"))
	require.Empty(t, r.elements["bn-backfill"])
}

// --- Add/Remove compound-set (reply-stamp) policies ---

func TestAdd_CompoundSetPolicy(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-backfill", Action: ActionStamp, Stamp: "reserve-egress", ReplyStamp: "backfill-response"},
		nil, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)

	require.NoError(t, m.Add(context.Background(), "bn-backfill", []string{"10.30.5.7:43473"}))
	// The element must be converted to the compound `ip . port` form.
	require.Equal(t, []string{"10.30.5.7 . 43473"}, r.elements["bn-backfill"])
}

func TestSet_CompoundSetPolicy(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-backfill", Action: ActionStamp, Stamp: "reserve-egress", ReplyStamp: "backfill-response"},
		[]string{"10.30.5.7:43473"}, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)

	require.NoError(t, m.Set(context.Background(), "bn-backfill", []string{"10.30.5.8:43473"}))
	require.Equal(t, []string{"10.30.5.8 . 43473"}, r.elements["bn-backfill"])
}

func TestShow_ReplyStampPolicy(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-backfill", Action: ActionStamp, Stamp: "reserve-egress", ReplyStamp: "backfill-response"},
		[]string{"10.30.5.7:43473"}, []string{"10.4.0.0/24"}, false)
	require.NoError(t, err)

	out, err := m.Show(context.Background(), "bn-backfill")
	require.NoError(t, err)
	require.Contains(t, out, "class:   reserve-egress")
	require.Contains(t, out, "reply-class: backfill-response")
	require.Contains(t, out, "live set @bn-backfill:")
	require.True(t, strings.Contains(out, "10.30.5.7 . 43473") || strings.Contains(out, "10.30.5.7:43473"),
		"show must display compound-set membership in some recognizable form")
}
