// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubSysctlFiles makes existing present before the copy, makes the
// copy write targets, and records what rollback asked to delete.
func stubSysctlFiles(t *testing.T, existing, targets []string) *[][]string {
	t.Helper()
	deleted := &[][]string{}
	origLstat, origTargets, origCopy, origDelete := lstatPath, sysctlConfigTargets, copySysctlConfiguration, deleteSysctlFiles
	present := make(map[string]bool, len(existing))
	for _, p := range existing {
		present[p] = true
	}
	lstatPath = func(p string) (os.FileInfo, error) {
		if present[p] {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
	sysctlConfigTargets = func() ([]string, error) { return targets, nil }
	copySysctlConfiguration = func() ([]string, error) { return targets, nil }
	deleteSysctlFiles = func(paths []string) ([]string, error) {
		*deleted = append(*deleted, paths)
		return paths, nil
	}
	t.Cleanup(func() {
		lstatPath, sysctlConfigTargets, copySysctlConfiguration, deleteSysctlFiles = origLstat, origTargets, origCopy, origDelete
	})
	return deleted
}

// Rollback scope: copy-sysctl-files removes only the files it created.

func TestCopySysctlFiles_AllPresentIsSkippedAndNotRolledBack(t *testing.T) {
	files := []string{"/etc/sysctl.d/75-inotify.conf", "/etc/sysctl.d/99-kubernetes-cri.conf"}
	deleted := stubSysctlFiles(t, files, files)

	step, err := copySysctlConfigurationFiles().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)
	assert.Equal(t, automa.StatusSkipped, report.Status)

	rollback := step.Rollback(context.Background())
	require.NoError(t, rollback.Error)
	assert.Equal(t, automa.StatusSkipped, rollback.Status)
	assert.Empty(t, *deleted, "files this run did not create must stay")
}

func TestCopySysctlFiles_RollbackRemovesOnlyCreatedFiles(t *testing.T) {
	existing := []string{"/etc/sysctl.d/75-inotify.conf", "/etc/sysctl.d/10-operator.conf"}
	copied := []string{"/etc/sysctl.d/75-inotify.conf", "/etc/sysctl.d/99-kubernetes-cri.conf"}
	deleted := stubSysctlFiles(t, existing, copied)

	step, err := copySysctlConfigurationFiles().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)
	assert.Equal(t, automa.StatusSuccess, report.Status)

	rollback := step.Rollback(context.Background())
	require.NoError(t, rollback.Error)
	assert.Equal(t, automa.StatusSuccess, rollback.Status)
	assert.Equal(t, [][]string{{"/etc/sysctl.d/99-kubernetes-cri.conf"}}, *deleted)
}

// A copy that fails partway has written whole files and possibly a partial one
// it does not report; the failed step's rollback must remove every new target.
func TestCopySysctlFiles_PartialCopyFailureIsRolledBack(t *testing.T) {
	targets := []string{"/etc/sysctl.d/75-inotify.conf", "/etc/sysctl.d/99-kubernetes-cri.conf"}
	deleted := stubSysctlFiles(t, nil, targets)
	copySysctlConfiguration = func() ([]string, error) {
		return targets[:1], assert.AnError
	}

	step, err := copySysctlConfigurationFiles().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.Error(t, report.Error)
	assert.Equal(t, automa.StatusFailed, report.Status)

	rollback := step.Rollback(context.Background())
	require.NoError(t, rollback.Error)
	assert.Equal(t, automa.StatusSuccess, rollback.Status)
	assert.Equal(t, [][]string{targets}, *deleted)
}

// Any entry already at a target path is not this run's, even one that is not a
// regular file: rollback must not remove a directory that was there before.
func TestAbsentPaths_KeepsExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	existingDir := filepath.Join(dir, "75-inotify.conf")
	require.NoError(t, os.Mkdir(existingDir, 0o755))
	missing := filepath.Join(dir, "99-kubernetes-cri.conf")

	absent, err := absentPaths([]string{existingDir, missing})
	require.NoError(t, err)
	assert.Equal(t, []string{missing}, absent)
}
