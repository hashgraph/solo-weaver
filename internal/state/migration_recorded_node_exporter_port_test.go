// SPDX-License-Identifier: Apache-2.0

package state

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRecordedPortHost points the state paths at a temp dir and returns it.
func newRecordedPortHost(t *testing.T) string {
	t.Helper()
	t.Cleanup(models.SetPaths(t.TempDir()))
	dir := models.Paths().StateDir
	require.NoError(t, os.MkdirAll(dir, 0o755))
	return dir
}

func writeRecordedFirewall(t *testing.T, dir string, fw *HostFirewallState) {
	t.Helper()
	sm, err := newStateManager(WithStateFile(filepath.Join(dir, StateFileName)))
	require.NoError(t, err)
	st := sm.State()
	st.MachineState.Firewall = fw
	sm.Set(st)
	require.NoError(t, sm.writeUnrecorded(ComponentFirewall))
}

func readRecordedFirewall(t *testing.T, dir string) *HostFirewallState {
	t.Helper()
	sm, err := newStateManager(WithStateFile(filepath.Join(dir, StateFileName)))
	require.NoError(t, err)
	require.NoError(t, sm.RefreshScoped(ComponentFirewall))
	return sm.State().MachineState.Firewall
}

func recordedPortMctx(installed, current string) *migration.Context {
	bag := &automa.SyncStateBag{}
	bag.Set(migration.CtxKeyInstalledCLIVersion, installed)
	bag.Set(migration.CtxKeyCurrentCLIVersion, current)
	return &migration.Context{Component: migration.ScopeStartup, Data: bag}
}

func TestRecordedNodeExporterPortMigration_Applies(t *testing.T) {
	m := NewRecordedNodeExporterPortMigration()
	for _, tc := range []struct {
		name               string
		installed, current string
		want               bool
	}{
		{"crossing the boundary", "0.36.1", recordedNodeExporterPortMinVersion, true},
		{"already past the boundary", recordedNodeExporterPortMinVersion, "0.38.0", false},
		{"fresh install", "", recordedNodeExporterPortMinVersion, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := m.Applies(recordedPortMctx(tc.installed, tc.current))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestRecordedNodeExporterPortMigration_AddsAndRollsBack(t *testing.T) {
	dir := newRecordedPortHost(t)
	writeRecordedFirewall(t, dir, &HostFirewallState{
		ManagementCIDRs: []string{"192.168.50.0/24"},
		InClusterPorts:  []int{6443, 4244, 7472, 10250},
	})
	m := NewRecordedNodeExporterPortMigration()
	ctx := context.Background()

	before := snapshotDir(t, dir)
	require.NoError(t, m.Execute(ctx, nil))
	executed := snapshotDir(t, dir)
	require.NotEqual(t, before, executed)
	requireDeclared(t, m.stateFiles(dir), before, executed)

	got := readRecordedFirewall(t, dir)
	assert.Equal(t, []int{6443, 4244, 7472, 10250, 9100}, got.InClusterPorts, "appended, so the order is kept")
	assert.Equal(t, []string{"192.168.50.0/24"}, got.ManagementCIDRs, "the rest of the record is kept")

	require.NoError(t, m.Rollback(ctx, nil))
	requireDeclared(t, m.stateFiles(dir), executed, snapshotDir(t, dir))
	assert.Equal(t, []int{6443, 4244, 7472, 10250}, readRecordedFirewall(t, dir).InClusterPorts)

	require.NoError(t, m.Rollback(ctx, nil), "a second rollback has nothing to undo")
	assert.Equal(t, []int{6443, 4244, 7472, 10250}, readRecordedFirewall(t, dir).InClusterPorts)
}

func TestRecordedNodeExporterPortMigration_Execute(t *testing.T) {
	for _, tc := range []struct {
		name   string
		record *HostFirewallState
		want   []int
	}{
		{"keeps operator ports", &HostFirewallState{InClusterPorts: []int{6443, 4244, 7472, 10250, 8080}}, []int{6443, 4244, 7472, 10250, 8080, 9100}},
		{"disabled firewall still gets it", &HostFirewallState{Disabled: true, InClusterPorts: []int{6443, 4244, 7472, 10250}}, []int{6443, 4244, 7472, 10250, 9100}},
		{"narrowed list is left alone", &HostFirewallState{InClusterPorts: []int{6443, 10250}}, []int{6443, 10250}},
		{"port already present", &HostFirewallState{InClusterPorts: []int{6443, 4244, 7472, 9100, 10250}}, []int{6443, 4244, 7472, 9100, 10250}},
		{"decision-only record gains nothing", &HostFirewallState{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := newRecordedPortHost(t)
			writeRecordedFirewall(t, dir, tc.record)

			require.NoError(t, NewRecordedNodeExporterPortMigration().Execute(context.Background(), nil))

			got := readRecordedFirewall(t, dir)
			assert.Equal(t, tc.want, got.InClusterPorts)
			assert.Equal(t, tc.record.Disabled, got.Disabled)
		})
	}
}

func TestRecordedNodeExporterPortMigration_NoRecord(t *testing.T) {
	dir := newRecordedPortHost(t)

	require.NoError(t, NewRecordedNodeExporterPortMigration().Execute(context.Background(), nil))

	assert.NoFileExists(t, componentFilePath(dir, ComponentFirewall))
}

// A record the migration cannot judge only warns: failing the startup pass
// would stop every command, including the ones that repair the record.
func TestRecordedNodeExporterPortMigration_InvalidRecordedPortWarns(t *testing.T) {
	dir := newRecordedPortHost(t)
	writeRecordedFirewall(t, dir, &HostFirewallState{InClusterPorts: []int{6443, 0}})
	m := NewRecordedNodeExporterPortMigration()

	require.NoError(t, m.Execute(context.Background(), nil))

	assert.Equal(t, []int{6443, 0}, readRecordedFirewall(t, dir).InClusterPorts)
	assert.False(t, m.added)
}

// A command that holds the firewall record past the wait makes the migration
// skip with a warning, not fail the startup pass.
func TestRecordedNodeExporterPortMigration_HeldLockWarns(t *testing.T) {
	dir := newRecordedPortHost(t)
	writeRecordedFirewall(t, dir, &HostFirewallState{InClusterPorts: []int{6443, 4244, 7472, 10250}})
	orig := recordedPortLockWait
	recordedPortLockWait = 0
	t.Cleanup(func() { recordedPortLockWait = orig })

	release, err := AcquireComponentLocks(dir, []ComponentID{ComponentFirewall}, 0)
	require.NoError(t, err)
	m := NewRecordedNodeExporterPortMigration()
	require.NoError(t, m.Execute(context.Background(), nil))
	release()

	assert.Equal(t, []int{6443, 4244, 7472, 10250}, readRecordedFirewall(t, dir).InClusterPorts)
	assert.False(t, m.added, "nothing was written, so Rollback must not remove anything")
}
