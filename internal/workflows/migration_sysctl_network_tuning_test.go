// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubDestPath points networkTuningDestPath at a file under t.TempDir() so tests
// never touch the real /etc/sysctl.d.
func stubDestPath(t *testing.T) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), networkPerformanceConfFile)
	orig := networkTuningDestPath
	t.Cleanup(func() { networkTuningDestPath = orig })
	networkTuningDestPath = dst
	return dst
}

// stubSysctlSeams replaces every host-facing seam with a recording fake and
// restores the originals on cleanup.
func stubSysctlSeams(t *testing.T) (copyCalls *int, reloadCalls *int, setCalls *map[string]string, getValues *map[string]string) {
	t.Helper()

	origCopy, origReload, origSet, origGet := copyNetworkTuningFile, reloadAllSysctlConfiguration, setSysctlValue, getSysctlValue
	t.Cleanup(func() {
		copyNetworkTuningFile = origCopy
		reloadAllSysctlConfiguration = origReload
		setSysctlValue = origSet
		getSysctlValue = origGet
	})

	copies := 0
	reloads := 0
	sets := map[string]string{}
	gets := map[string]string{
		keepaliveProbesKey: wantKeepaliveProbesValue,
	}

	copyNetworkTuningFile = func(name string) (string, error) {
		copies++
		return networkTuningDestPath, nil
	}
	reloadAllSysctlConfiguration = func() ([]string, error) {
		reloads++
		return nil, nil
	}
	setSysctlValue = func(key, value string) error {
		sets[key] = value
		return nil
	}
	getSysctlValue = func(key string) (string, error) {
		return gets[key], nil
	}

	return &copies, &reloads, &sets, &gets
}

func TestSysctlNetworkTuningMigration_Metadata(t *testing.T) {
	m := NewSysctlNetworkTuningMigration()
	assert.Equal(t, "sysctl-network-tuning-v"+sysctlNetworkTuningMinVersion, m.ID())
	assert.Contains(t, m.Description(), "fs.file-max")
}

// TestSysctlNetworkTuningMigration_Applies pins the CLI-version-boundary gating
// inherited from migration.CLIVersionMigration — same shape as every other
// version-gated startup migration (e.g. TestCrioSocketDropInMigration_Applies).
func TestSysctlNetworkTuningMigration_Applies(t *testing.T) {
	m := NewSysctlNetworkTuningMigration()

	tests := []struct {
		name      string
		installed string
		current   string
		want      bool
	}{
		{"upgrade across boundary applies", "0.32.0", sysctlNetworkTuningMinVersion, true},
		{"upgrade from below to above applies", "0.0.0", "0.33.0", true},
		{"fresh install does not apply", "", sysctlNetworkTuningMinVersion, false},
		{"already past boundary does not apply", sysctlNetworkTuningMinVersion, "0.33.0", false},
		{"below boundary on both sides does not apply", "0.31.0", "0.32.0", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := m.Applies(newMctx(tc.installed, tc.current))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestSysctlNetworkTuningMigration_Execute(t *testing.T) {
	m := NewSysctlNetworkTuningMigration()

	t.Run("happy path: copies, reloads all of /etc/sysctl.d, restores fs.file-max from backup", func(t *testing.T) {
		dst := stubDestPath(t)
		require.NoError(t, os.WriteFile(dst, []byte("old contents\n"), models.DefaultFilePerm))
		copies, reloads, sets, _ := stubSysctlSeams(t)

		restoreBackupDir := models.SetPaths(t.TempDir())
		t.Cleanup(restoreBackupDir)
		backupFile := path.Join(models.Paths().BackupDir, steps.SysCtlBackupFilename)
		require.NoError(t, os.MkdirAll(path.Dir(backupFile), models.DefaultDirOrExecPerm))
		require.NoError(t, os.WriteFile(backupFile, []byte("fs.file-max = 9223372036854775807\n"), models.DefaultFilePerm))

		mctx := newMctx("0.28.1", "0.29.0")
		require.NoError(t, m.Execute(context.Background(), mctx))

		assert.Equal(t, 1, *copies)
		assert.Equal(t, 1, *reloads)
		assert.Equal(t, "9223372036854775807", (*sets)[fileMaxKey])

		prev, ok := mctx.Data.Get(sysctlNetworkTuningPrevContentsKey)
		require.True(t, ok, "the pre-migration file bytes must be stashed for rollback")
		assert.Equal(t, []byte("old contents\n"), prev)
	})

	t.Run("unreadable file is a hard error, not a silent skip", func(t *testing.T) {
		dst := stubDestPath(t)
		require.NoError(t, os.Mkdir(dst, models.DefaultDirOrExecPerm)) // a dir where a file is expected -> read error
		stubSysctlSeams(t)
		restoreBackupDir := models.SetPaths(t.TempDir())
		t.Cleanup(restoreBackupDir)

		err := m.Execute(context.Background(), newMctx("0.28.1", "0.29.0"))
		require.Error(t, err)
	})

	t.Run("no previous file: nothing stashed for rollback", func(t *testing.T) {
		stubDestPath(t) // file does not exist
		stubSysctlSeams(t)
		restoreBackupDir := models.SetPaths(t.TempDir())
		t.Cleanup(restoreBackupDir)

		mctx := newMctx("0.28.1", "0.29.0")
		require.NoError(t, m.Execute(context.Background(), mctx))

		_, ok := mctx.Data.Get(sysctlNetworkTuningPrevContentsKey)
		assert.False(t, ok)
	})

	t.Run("copy failure is returned with a resolution hint", func(t *testing.T) {
		dst := stubDestPath(t)
		require.NoError(t, os.WriteFile(dst, []byte("old contents\n"), models.DefaultFilePerm))
		stubSysctlSeams(t)
		copyNetworkTuningFile = func(name string) (string, error) {
			return "", errorx.ExternalError.New("read-only fs")
		}
		restoreBackupDir := models.SetPaths(t.TempDir())
		t.Cleanup(restoreBackupDir)

		err := m.Execute(context.Background(), newMctx("0.28.1", "0.29.0"))
		require.Error(t, err)
	})

	t.Run("reload failure is returned with a resolution hint", func(t *testing.T) {
		dst := stubDestPath(t)
		require.NoError(t, os.WriteFile(dst, []byte("old contents\n"), models.DefaultFilePerm))
		stubSysctlSeams(t)
		reloadAllSysctlConfiguration = func() ([]string, error) {
			return nil, errorx.ExternalError.New("bad value")
		}
		restoreBackupDir := models.SetPaths(t.TempDir())
		t.Cleanup(restoreBackupDir)

		err := m.Execute(context.Background(), newMctx("0.28.1", "0.29.0"))
		require.Error(t, err)
	})

	t.Run("missing backup: fs.file-max restore is skipped, Execute still succeeds", func(t *testing.T) {
		dst := stubDestPath(t)
		require.NoError(t, os.WriteFile(dst, []byte("old contents\n"), models.DefaultFilePerm))
		_, _, sets, _ := stubSysctlSeams(t)

		restoreBackupDir := models.SetPaths(t.TempDir()) // BackupDir exists but has no sysctl.conf
		t.Cleanup(restoreBackupDir)

		err := m.Execute(context.Background(), newMctx("0.28.1", "0.29.0"))
		require.NoError(t, err, "a missing backup is best-effort, not a migration failure")
		assert.NotContains(t, *sets, fileMaxKey, "no backup value to restore, so Set must not be called for fs.file-max")
	})

	t.Run("tcp_keepalive_probes mismatch after reload is reported", func(t *testing.T) {
		dst := stubDestPath(t)
		require.NoError(t, os.WriteFile(dst, []byte("old contents\n"), models.DefaultFilePerm))
		_, _, _, gets := stubSysctlSeams(t)
		(*gets)[keepaliveProbesKey] = "9" // kernel default, migration did not take effect

		restoreBackupDir := models.SetPaths(t.TempDir())
		t.Cleanup(restoreBackupDir)

		err := m.Execute(context.Background(), newMctx("0.28.1", "0.29.0"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), keepaliveProbesKey)
	})
}

func TestSysctlNetworkTuningMigration_Rollback(t *testing.T) {
	m := NewSysctlNetworkTuningMigration()

	t.Run("restores the stashed previous file and reloads", func(t *testing.T) {
		dst := stubDestPath(t)
		_, reloads, _, _ := stubSysctlSeams(t)

		mctx := newMctx("0.28.1", "0.29.0")
		mctx.Data.Set(sysctlNetworkTuningPrevContentsKey, []byte("old contents\n"))

		require.NoError(t, m.Rollback(context.Background(), mctx))

		got, err := os.ReadFile(dst)
		require.NoError(t, err)
		assert.Equal(t, "old contents\n", string(got))
		assert.Equal(t, 1, *reloads)
	})

	t.Run("no-op when nothing was stashed", func(t *testing.T) {
		dst := stubDestPath(t)
		_, reloads, _, _ := stubSysctlSeams(t)

		require.NoError(t, m.Rollback(context.Background(), newMctx("0.28.1", "0.29.0")))

		_, err := os.ReadFile(dst)
		assert.True(t, os.IsNotExist(err), "rollback must not create a file that never existed")
		assert.Equal(t, 0, *reloads)
	})
}
