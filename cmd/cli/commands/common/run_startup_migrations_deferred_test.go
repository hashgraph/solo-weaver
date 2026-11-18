// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"os"
	"testing"

	"github.com/automa-saga/version"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// apiGatedMigration defers until apiUp is set, then applies.
type apiGatedMigration struct {
	fakeStartupMigration
	apiUp *bool
}

func (m *apiGatedMigration) Execute(ctx context.Context, mctx *migration.Context) error {
	if !*m.apiUp {
		return migration.ErrDeferred.New("the Kubernetes API is not available")
	}
	return m.fakeStartupMigration.Execute(ctx, mctx)
}

// TestRunStartupMigrations_DeferredMigrationRunsOnALaterCommand is the issue's
// scenario: the first command after an upgrade meets a down API, so the
// migration defers, the command still runs, and the version stays where it
// was, across a later upgrade too; the first command after the API returns
// applies it.
func TestRunStartupMigrations_DeferredMigrationRunsOnALaterCommand(t *testing.T) {
	migration.ClearRegistry()
	t.Cleanup(migration.ClearRegistry)
	origVersion := version.Version
	t.Cleanup(func() { version.Version = origVersion })
	version.Version = "1.2.3"
	t.Cleanup(models.SetPaths(t.TempDir()))
	require.NoError(t, os.MkdirAll(models.Paths().StateDir, 0o755))
	origK8s := workflows.KubernetesInstalled
	t.Cleanup(func() { workflows.KubernetesInstalled = origK8s })
	workflows.KubernetesInstalled = func() bool { return true }
	_ = state.TakeWarnings()

	applied, apiUp := 0, false
	migration.Register(migration.ScopeStartup, &apiGatedMigration{
		fakeStartupMigration: fakeStartupMigration{id: "cilium-host-legacy-routing-v0.21.0", applied: &applied},
		apiUp:                &apiUp,
	})
	ctx := context.Background()

	require.NoError(t, RunStartupMigrations(ctx), "a deferral must not block the command")
	recorded, err := state.ReadProvisionerVersionFromDisk()
	require.NoError(t, err)
	assert.Empty(t, recorded, "the version must not be recorded while a migration is deferred")
	warnings := state.TakeWarnings()
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "cilium-host-legacy-routing-v0.21.0")

	require.NoError(t, RunStartupMigrations(ctx))
	assert.Zero(t, applied, "still deferred while the API is down")
	assert.Len(t, state.TakeWarnings(), 1, "every command warns until the deferral clears")

	// The operator upgrades again before the API comes back: the deferral
	// carries over to the new binary.
	version.Version = "1.3.0"
	require.NoError(t, RunStartupMigrations(ctx))
	assert.Zero(t, applied)
	assert.Len(t, state.TakeWarnings(), 1)

	apiUp = true
	require.NoError(t, RunStartupMigrations(ctx))
	assert.Equal(t, 1, applied, "the first command after the API returns applies it")
	recorded, err = state.ReadProvisionerVersionFromDisk()
	require.NoError(t, err)
	assert.Equal(t, "1.3.0", recorded)
	assert.Empty(t, state.TakeWarnings())
}
