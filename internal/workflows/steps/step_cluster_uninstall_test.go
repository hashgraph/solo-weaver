// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/golang/mock/gomock"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/security/principal"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubRemoveShellCompletion scripts the loader removal and reports whether it ran.
func stubRemoveShellCompletion(t *testing.T, err error) *bool {
	t.Helper()

	orig := removeShellCompletion
	t.Cleanup(func() { removeShellCompletion = orig })

	called := false
	removeShellCompletion = func() error {
		called = true
		return err
	}

	return &called
}

func runRemoveLoadersStep(t *testing.T) *automa.Report {
	t.Helper()

	step, err := RemoveShellCompletionLoaders().Build()
	require.NoError(t, err)

	return step.Execute(context.Background())
}

func TestRemoveShellCompletionLoaders_RemovesThem(t *testing.T) {
	called := stubRemoveShellCompletion(t, nil)

	report := runRemoveLoadersStep(t)

	assert.True(t, *called)
	assert.Equal(t, automa.StatusSuccess, report.Status)
	require.NoError(t, report.Error)
}

// Teardown is best-effort, so a removal failure must not fail the workflow —
// but it must not be reported as success either, or the run claims work it did
// not do.
func TestRemoveShellCompletionLoaders_FailureIsSkippedNotFailed(t *testing.T) {
	called := stubRemoveShellCompletion(t, errorx.ExternalError.New("read-only file system"))

	report := runRemoveLoadersStep(t)

	assert.True(t, *called)
	assert.Equal(t, automa.StatusSkipped, report.Status)
	require.NoError(t, report.Error, "a skipped teardown step must not carry an error")
}

// TestCleanupWeaverHome_KeepsTheActionHistory: the cleanup empties the state
// directory but keeps action_history.yaml, which records the uninstall too,
// and still keeps the downloads, bin and logs folders.
func TestCleanupWeaverHome_KeepsTheActionHistory(t *testing.T) {
	home := t.TempDir()
	t.Cleanup(models.SetPaths(home))
	paths := models.Paths()
	for _, d := range []string{paths.StateDir, paths.DownloadsDir, paths.BinDir, paths.LogsDir, filepath.Join(home, "config")} {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}
	history := state.ActionHistoryFilePath(paths.StateDir)
	for _, f := range []string{history, filepath.Join(paths.StateDir, "blocknode.yaml"), filepath.Join(paths.StateDir, "blocknode.lock")} {
		require.NoError(t, os.WriteFile(f, []byte("x"), 0o644))
	}
	fm, err := fsx.NewManager(fsx.WithPrincipalManager(principal.NewMockManager(gomock.NewController(t))))
	require.NoError(t, err)

	cleanupWeaverHome(fm, home)

	entries, err := os.ReadDir(paths.StateDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, state.ActionHistoryFileName, entries[0].Name())
	for _, kept := range []string{paths.DownloadsDir, paths.BinDir, paths.LogsDir} {
		require.DirExists(t, kept)
	}
	require.NoDirExists(t, filepath.Join(home, "config"))
}
