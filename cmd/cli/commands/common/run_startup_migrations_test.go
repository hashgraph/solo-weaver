// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/automa-saga/version"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// fakeStartupMigration applies only at the 0.0.0 baseline and counts each Execute.
type fakeStartupMigration struct {
	id      string
	applied *int
}

func (m *fakeStartupMigration) ID() string          { return m.id }
func (m *fakeStartupMigration) Description() string { return "fake startup migration" }
func (m *fakeStartupMigration) Applies(mctx *migration.Context) (bool, error) {
	installed, _ := mctx.Data.String(migration.CtxKeyInstalledCLIVersion)
	return installed == migration.BaselineCLIVersion, nil
}
func (m *fakeStartupMigration) Execute(_ context.Context, _ *migration.Context) error {
	*m.applied++
	return nil
}
func (m *fakeStartupMigration) Rollback(_ context.Context, _ *migration.Context) error { return nil }

// registerFakeStartupMigration isolates the registry to one fake migration under a temp state path; returns the execute counter.
func registerFakeStartupMigration(t *testing.T) *int {
	t.Helper()

	migration.ClearRegistry()
	t.Cleanup(migration.ClearRegistry)

	home := t.TempDir()
	t.Cleanup(models.SetPaths(home))
	// Provisioned host: state dir exists but no state.yaml → reader returns "".
	require.NoError(t, os.MkdirAll(models.Paths().StateDir, 0o755))

	applied := 0
	migration.Register(migration.ScopeStartup, &fakeStartupMigration{id: "fake-startup-v1", applied: &applied})
	return &applied
}

// TestRunStartupMigrations_PersistsVersionAndStopsRerun: run 1 (no state.yaml) crosses the baseline and records the version; run 2 reads it back, so the migration runs exactly once
func TestRunStartupMigrations_PersistsVersionAndStopsRerun(t *testing.T) {
	applied := registerFakeStartupMigration(t)

	// Force the provisioned-host gate true; the real probe checks
	// /etc/kubernetes/admin.conf, which is absent on a CI/test host.
	origK8s := workflows.KubernetesInstalled
	t.Cleanup(func() { workflows.KubernetesInstalled = origK8s })
	workflows.KubernetesInstalled = func() bool { return true }

	ctx := context.Background()

	// Run 1: absent state.yaml → "" → 0.0.0 baseline → boundary applies → runs once.
	require.NoError(t, RunStartupMigrations(ctx))
	assert.Equal(t, 1, *applied, "run 1 must execute the boundary migration once")

	// The orchestrator recorded the running version at the reader's path.
	recorded, err := state.ReadProvisionerVersionFromDisk()
	require.NoError(t, err)
	assert.Equal(t, version.Get().Version, recorded, "run 1 must persist the running version")

	// Run 2: the reader now returns the recorded version → not the baseline → the
	// boundary no longer applies, so the migration is not re-run.
	require.NoError(t, RunStartupMigrations(ctx))
	assert.Equal(t, 1, *applied, "run 2 must NOT re-execute the migration")
}

// TestRunStartupMigrations_SkipsPersistWhenKubernetesAbsent: without Kubernetes the version is not recorded, so the boundary re-runs next time.
func TestRunStartupMigrations_SkipsPersistWhenKubernetesAbsent(t *testing.T) {
	applied := registerFakeStartupMigration(t)

	origK8s := workflows.KubernetesInstalled
	t.Cleanup(func() { workflows.KubernetesInstalled = origK8s })
	workflows.KubernetesInstalled = func() bool { return false }

	ctx := context.Background()

	require.NoError(t, RunStartupMigrations(ctx))
	assert.Equal(t, 1, *applied, "run 1 still runs the migration")

	// Gate closed: nothing recorded, so the reader still sees an empty version.
	recorded, err := state.ReadProvisionerVersionFromDisk()
	require.NoError(t, err)
	assert.Empty(t, recorded, "version must NOT be persisted on a non-provisioned host")

	// Without a recorded version the boundary applies again on the next run.
	require.NoError(t, RunStartupMigrations(ctx))
	assert.Equal(t, 2, *applied, "without a recorded version the migration re-runs")
}

// TestRunStartupMigrations_PersistsVersionWhenNoMigrationApplies: the version is backfilled even when no migration applies — recording it must not depend on a migration running.
func TestRunStartupMigrations_PersistsVersionWhenNoMigrationApplies(t *testing.T) {
	// Isolate an empty registry at a temp home with a state dir but no state.yaml.
	migration.ClearRegistry()
	t.Cleanup(migration.ClearRegistry)
	home := t.TempDir()
	t.Cleanup(models.SetPaths(home))
	require.NoError(t, os.MkdirAll(models.Paths().StateDir, 0o755))

	origK8s := workflows.KubernetesInstalled
	t.Cleanup(func() { workflows.KubernetesInstalled = origK8s })
	workflows.KubernetesInstalled = func() bool { return true }

	// Precondition: no applicable migrations and an absent version on disk.
	before, err := state.ReadProvisionerVersionFromDisk()
	require.NoError(t, err)
	require.Empty(t, before)

	require.NoError(t, RunStartupMigrations(context.Background()))

	recorded, err := state.ReadProvisionerVersionFromDisk()
	require.NoError(t, err)
	assert.Equal(t, version.Get().Version, recorded,
		"version must be backfilled even when no migration applied")
}

// TestRunStartupMigrations_PersistFailureIsNonFatal: recording the version is
// best-effort — if it fails (e.g. a corrupt state.yaml or an unwritable state dir
// on a long-lived host), startup must still succeed. Regression guard: turning the
// swallowed Warn into a returned error would abort every CLI invocation on such a
// host, and quietly re-run boundary migrations forever. See #789.
func TestRunStartupMigrations_PersistFailureIsNonFatal(t *testing.T) {
	applied := registerFakeStartupMigration(t)

	origK8s := workflows.KubernetesInstalled
	t.Cleanup(func() { workflows.KubernetesInstalled = origK8s })
	workflows.KubernetesInstalled = func() bool { return true }

	// Force the version record to fail the way a wonky state dir would.
	origPersist := persistProvisionerVersion
	t.Cleanup(func() { persistProvisionerVersion = origPersist })
	persistCalled := 0
	persistProvisionerVersion = func(_ ...state.ManagerOption) error {
		persistCalled++
		return errors.New("simulated persist failure")
	}

	// The migration applies (baseline crosses the boundary) and the persist fails —
	// RunStartupMigrations must swallow the error and still return nil.
	require.NoError(t, RunStartupMigrations(context.Background()),
		"a failed version record must not fail startup")
	assert.Equal(t, 1, persistCalled, "the version record must have been attempted")
	assert.Equal(t, 1, *applied, "the migration must still have executed despite the persist failure")
}

// TestRunStartupMigrations_ConcurrentInvocationsSplitStateOnce starts several
// invocations at once on a host with a legacy state.yaml. The split deletes
// and renames state files, so without serialization the losers fail reading a
// state.yaml the winner already renamed, or clear files the winner just wrote.
// Every invocation must succeed and the split must land intact.
func TestRunStartupMigrations_ConcurrentInvocationsSplitStateOnce(t *testing.T) {
	migration.ClearRegistry()
	t.Cleanup(migration.ClearRegistry)
	home := t.TempDir()
	t.Cleanup(models.SetPaths(home))
	dir := models.Paths().StateDir
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, state.StateFileName), []byte(`state:
  machineState:
    profile: mainnet
  blockNodeState:
    name: block-node
`), 0o644))
	migration.Register(migration.ScopeStartup, state.NewPerComponentStateMigration())

	origK8s := workflows.KubernetesInstalled
	t.Cleanup(func() { workflows.KubernetesInstalled = origK8s })
	workflows.KubernetesInstalled = func() bool { return false }

	const invocations = 8
	errs := make(chan error, invocations)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < invocations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- RunStartupMigrations(context.Background())
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	_, err := os.Stat(filepath.Join(dir, state.StateFileName))
	require.True(t, os.IsNotExist(err), "state.yaml must be split and moved out of the way")
	_, err = os.Stat(filepath.Join(dir, state.StateFileName+".legacy"))
	require.NoError(t, err, "the split must have kept its backup")
	diverged, err := filepath.Glob(filepath.Join(dir, state.StateFileName+".diverged-*"))
	require.NoError(t, err)
	require.Empty(t, diverged)

	read := func(id state.ComponentID) state.State {
		b, err := os.ReadFile(state.ComponentFilePath(dir, id))
		require.NoError(t, err, "%s state file must exist", id)
		var s state.State
		require.NoError(t, yaml.Unmarshal(b, &s))
		return s
	}
	assert.Equal(t, "mainnet", read(state.ComponentMachine).MachineState.Profile)
	assert.Equal(t, "block-node", read(state.ComponentBlockNode).BlockNodeState.ReleaseInfo.Name)
}
