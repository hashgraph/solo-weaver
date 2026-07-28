// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// seedManagedPortsPolicy creates a --stamp policy whose listener ports are
// daemon-managed (empty <name>_ports set at render, filled by ApplyPorts).
func seedManagedPortsPolicy(t *testing.T, m *Manager, name, stamp string) {
	t.Helper()
	_, err := m.Create(context.Background(),
		&Policy{Name: name, Action: ActionStamp, Stamp: stamp, ManagedPorts: true},
		nil, "10.4.0.0/24", false)
	require.NoError(t, err)
}

func TestApplyPorts_WritesManagedPortsSet(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedManagedPortsPolicy(t, m, "bn-publisher", "publisher")

	applied, err := m.ApplyPorts(context.Background(), map[string][]string{
		"bn-publisher": {"40984"},
	})
	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, []string{"40984"}, r.elements["bn-publisher_ports"],
		"ApplyPorts writes the <name>_ports set, not the membership set")
	require.Empty(t, r.elements["bn-publisher"], "the CIDR membership set is untouched")
}

func TestApplyPorts_EmptyClearsPortsSet(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedManagedPortsPolicy(t, m, "bn-publisher", "publisher")

	_, err := m.ApplyPorts(context.Background(), map[string][]string{"bn-publisher": {"40984"}})
	require.NoError(t, err)
	_, err = m.ApplyPorts(context.Background(), map[string][]string{"bn-publisher": {}})
	require.NoError(t, err)
	require.Empty(t, r.elements["bn-publisher_ports"], "an empty desired list clears the ports set")
}

func TestApplyPorts_RejectsNonManagedPolicy(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	// A static-ports policy is not daemon-managed; ApplyPorts must refuse it.
	seedPolicy(t, m, "bn-mgmt", "reserve-ingress", []string{"40983"}, nil, "10.4.0.0/24")

	_, err := m.ApplyPorts(context.Background(), map[string][]string{"bn-mgmt": {"40983"}})
	require.ErrorContains(t, err, "no daemon-managed ports set")
}

func TestApplyPorts_RejectsInvalidPort(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedManagedPortsPolicy(t, m, "bn-publisher", "publisher")

	_, err := m.ApplyPorts(context.Background(), map[string][]string{"bn-publisher": {"99999999"}})
	require.ErrorContains(t, err, "invalid listener port")
	require.Empty(t, r.elements["bn-publisher_ports"], "an invalid port must not be written")
}

func TestApplySets_WritesBothDimensionsUnderOneLock(t *testing.T) {
	r := newFakeRunner()
	m, _, _ := newTestManager(t, r)
	seedManagedPortsPolicy(t, m, "bn-publisher", "publisher")

	applied, err := m.ApplySets(context.Background(),
		map[string][]string{"bn-publisher": {"10.1.0.1/32"}},
		map[string][]string{"bn-publisher": {"40984"}})
	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, []string{"10.1.0.1/32"}, r.elements["bn-publisher"], "membership set written")
	require.Equal(t, []string{"40984"}, r.elements["bn-publisher_ports"], "ports set written")
}

func TestApplySets_SkipsBothDimensionsWhenLockHeld(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)
	seedManagedPortsPolicy(t, m, "bn-publisher", "publisher")

	lf, err := os.OpenFile(lockPathFor(nftPath), os.O_CREATE|os.O_RDWR, 0o600)
	require.NoError(t, err)
	defer func() { _ = lf.Close() }()
	require.NoError(t, syscall.Flock(int(lf.Fd()), syscall.LOCK_EX))
	defer func() { _ = syscall.Flock(int(lf.Fd()), syscall.LOCK_UN) }()

	applied, err := m.ApplySets(context.Background(),
		map[string][]string{"bn-publisher": {"10.1.0.1/32"}},
		map[string][]string{"bn-publisher": {"40984"}})
	require.NoError(t, err, "a held lock is not an error — the whole tick is skipped")
	require.False(t, applied)
	require.Empty(t, r.elements["bn-publisher"], "membership not written when the tick is skipped")
	require.Empty(t, r.elements["bn-publisher_ports"], "ports not written either — the skip is atomic")
}

func TestApplyPorts_SkipsWhenLockHeld(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)
	seedManagedPortsPolicy(t, m, "bn-publisher", "publisher")

	lf, err := os.OpenFile(lockPathFor(nftPath), os.O_CREATE|os.O_RDWR, 0o600)
	require.NoError(t, err)
	defer func() { _ = lf.Close() }()
	require.NoError(t, syscall.Flock(int(lf.Fd()), syscall.LOCK_EX))
	defer func() { _ = syscall.Flock(int(lf.Fd()), syscall.LOCK_UN) }()

	applied, err := m.ApplyPorts(context.Background(), map[string][]string{"bn-publisher": {"40984"}})
	require.NoError(t, err, "a held lock is not an error — the tick is skipped")
	require.False(t, applied)
	require.Empty(t, r.elements["bn-publisher_ports"], "nothing is written when the tick is skipped")
}
