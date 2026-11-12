// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"strings"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
)

// TestRegisterMigrations_AgentRestartFollowsEveryCiliumChange: the restart
// defers when an earlier migration in the pass deferred, so it must run after
// every Cilium migration that can defer, or it rolls the agents on every
// command until that deferral clears (#1315).
func TestRegisterMigrations_AgentRestartFollowsEveryCiliumChange(t *testing.T) {
	migration.ClearRegistry()
	t.Cleanup(func() {
		migration.ClearRegistry()
		RegisterMigrations()
	})
	t.Cleanup(models.SetPaths(t.TempDir()))
	require.NoError(t, os.MkdirAll(models.Paths().StateDir, 0o755))
	RegisterMigrations()

	mctx := &migration.Context{Component: migration.ScopeStartup, Data: &automa.SyncStateBag{}}
	mctx.Data.Set(migration.CtxKeyInstalledCLIVersion, migration.BaselineCLIVersion)
	mctx.Data.Set(migration.CtxKeyCurrentCLIVersion, "99.0.0")
	applicable, err := migration.GetApplicableMigrations(migration.ScopeStartup, mctx)
	require.NoError(t, err)

	var cilium []string
	for _, m := range applicable {
		if strings.HasPrefix(m.ID(), "cilium-") {
			cilium = append(cilium, m.ID())
		}
	}
	require.Equal(t, []string{
		"cilium-disable-xdp-acceleration-v0.19.1",
		"cilium-host-legacy-routing-v0.21.0",
		"cilium-agent-restart-disabled-acceleration-v0.19.2",
	}, cilium)
}
