// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubSysctlFiles makes /etc/sysctl.d hold existing before the copy, makes the
// copy report copied, and records what rollback asked to delete.
func stubSysctlFiles(t *testing.T, existing, copied []string) *[][]string {
	t.Helper()
	deleted := &[][]string{}
	origFind, origCopy, origDelete := findSysctlConfigFiles, copySysctlConfiguration, deleteSysctlFiles
	findSysctlConfigFiles = func() ([]string, error) { return existing, nil }
	copySysctlConfiguration = func() ([]string, error) { return copied, nil }
	deleteSysctlFiles = func(paths []string) ([]string, error) {
		*deleted = append(*deleted, paths)
		return paths, nil
	}
	t.Cleanup(func() {
		findSysctlConfigFiles, copySysctlConfiguration, deleteSysctlFiles = origFind, origCopy, origDelete
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

// A copy that fails partway has already written files; the failed step's
// rollback must remove exactly those.
func TestCopySysctlFiles_PartialCopyFailureIsRolledBack(t *testing.T) {
	deleted := stubSysctlFiles(t, nil, nil)
	copySysctlConfiguration = func() ([]string, error) {
		return []string{"/etc/sysctl.d/75-inotify.conf"}, assert.AnError
	}

	step, err := copySysctlConfigurationFiles().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.Error(t, report.Error)
	assert.Equal(t, automa.StatusFailed, report.Status)

	rollback := step.Rollback(context.Background())
	require.NoError(t, rollback.Error)
	assert.Equal(t, automa.StatusSuccess, rollback.Status)
	assert.Equal(t, [][]string{{"/etc/sysctl.d/75-inotify.conf"}}, *deleted)
}
