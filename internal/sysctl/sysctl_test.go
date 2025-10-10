// +go:build integration
package sysctl

import (
	"os"
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/internal/core"
)

func Test_CopySysctlConfigurationFiles_Integration(t *testing.T) {
	files, err := CopyConfiguration()
	require.NoError(t, err)
	require.NotEmpty(t, files)

	files2, err := FindSysctlConfigFiles()
	require.NoError(t, err)
	require.Equal(t, len(files), len(files2))
	for _, f := range files {
		require.Contains(t, files2, f)
	}
}

func Test_RemoveSysctlConfigurationFiles_Integration(t *testing.T) {
	files, err := CopyConfiguration()
	require.NoError(t, err)
	require.NotEmpty(t, files)

	files2, err := FindSysctlConfigFiles()
	require.NoError(t, err)
	require.Equal(t, len(files), len(files2))
	for _, f := range files {
		require.Contains(t, files2, f)
	}

	removed, err := DeleteConfiguration()
	require.NoError(t, err)
	require.Equal(t, len(files), len(removed))
	for _, f := range files {
		require.Contains(t, removed, f)
	}

	files3, err := FindSysctlConfigFiles()
	require.NoError(t, err)
	require.Equal(t, 0, len(files3))
}

func Test_BackupSysctlConfiguration_Integration(t *testing.T) {
	backupFile := path.Join(core.Paths().BackupDir, "sysctl.conf")
	_ = os.Remove(backupFile)

	defer func() {
		_ = os.Remove(backupFile)
	}()

	backupFile, err := BackupSettings(backupFile)
	require.NoError(t, err)
	require.NotEmpty(t, backupFile)
	require.FileExists(t, backupFile)

	info, err := os.Stat(backupFile)
	require.NoError(t, err)
	require.True(t, info.Size() > 0)

	// check the contents
	data, err := os.ReadFile(backupFile)
	require.NoError(t, err)
	s := string(data)
	require.Equal(t, 29, len(strings.Split(s, "\n"))) // we are setting 31 configuration items
}
