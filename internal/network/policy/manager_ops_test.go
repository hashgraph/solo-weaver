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

// seedPolicy creates a policy via Create and verifies no error.
func seedPolicy(t *testing.T, m *Manager, name, stamp string, ports []string, cidrs []string, podCIDR string) {
	t.Helper()
	_, err := m.Create(context.Background(),
		&Policy{Name: name, Action: ActionStamp, Stamp: stamp, Ports: ports},
		cidrs, podCIDR, false)
	require.NoError(t, err)
}

func seedDenyPolicy(t *testing.T, m *Manager, name string, cidrs []string) {
	t.Helper()
	_, err := m.Create(context.Background(),
		&Policy{Name: name, Action: ActionDeny},
		cidrs, "", false)
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
		nil, "10.4.0.0/24", false)
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
		nil, "10.4.0.0/24", false)
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

func TestDelete_LastPolicy_AppliesEmptyChain(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)
	seedDenyPolicy(t, m, "bn-restricted", []string{"10.99.0.0/16"})

	require.NoError(t, m.Delete(context.Background(), "bn-restricted"))

	// The table is still live (empty chain, policy drop, no rules).
	require.True(t, r.exists, "table must still exist after last policy is deleted")

	// The .nft file was updated and no longer contains the deleted policy.
	onDisk, err := os.ReadFile(nftPath)
	require.NoError(t, err)
	require.NotContains(t, string(onDisk), "bn-restricted")
	// Chain structure is intact with policy drop.
	require.Contains(t, string(onDisk), "policy drop")
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
		[]string{"10.30.5.7:43473"}, "10.4.0.0/24", false)
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
		nil, "10.4.0.0/24", false)
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
		[]string{"10.30.5.7:43473"}, "10.4.0.0/24", false)
	require.NoError(t, err)

	require.NoError(t, m.Set(context.Background(), "bn-backfill", []string{"10.30.5.8:43473"}))
	require.Equal(t, []string{"10.30.5.8 . 43473"}, r.elements["bn-backfill"])
}

func TestShow_ReplyStampPolicy(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	_, err := m.Create(context.Background(),
		&Policy{Name: "bn-backfill", Action: ActionStamp, Stamp: "reserve-egress", ReplyStamp: "backfill-response"},
		[]string{"10.30.5.7:43473"}, "10.4.0.0/24", false)
	require.NoError(t, err)

	out, err := m.Show(context.Background(), "bn-backfill")
	require.NoError(t, err)
	require.Contains(t, out, "class:   reserve-egress")
	require.Contains(t, out, "reply-class: backfill-response")
	require.Contains(t, out, "live set @bn-backfill:")
	require.True(t, strings.Contains(out, "10.30.5.7 . 43473") || strings.Contains(out, "10.30.5.7:43473"),
		"show must display compound-set membership in some recognizable form")
}
