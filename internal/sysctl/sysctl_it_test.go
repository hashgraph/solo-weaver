// SPDX-License-Identifier: Apache-2.0

//go:build integration

package sysctl

import (
	"os"
	"path"
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
)

func Test_CopySysctlConfigurationFiles_Integration(t *testing.T) {
	// Record pre-existing config files
	existing, err := FindSysctlConfigFiles()
	require.NoError(t, err)

	// Copy new config files
	files, err := CopyConfiguration()
	require.NoError(t, err)
	require.NotEmpty(t, files)

	// Ensure new files are present in the updated list
	files2, err := FindSysctlConfigFiles()
	require.NoError(t, err)
	for _, f := range files {
		require.Contains(t, files2, f)
	}

	// Pre-existing files should still exist
	for _, f := range existing {
		require.Contains(t, files2, f)
	}
}

func Test_RemoveSysctlConfigurationFiles_Integration(t *testing.T) {
	// Copy new config files
	files, err := CopyConfiguration()
	require.NoError(t, err)
	require.NotEmpty(t, files)

	// Ensure new files are present
	files2, err := FindSysctlConfigFiles()
	require.NoError(t, err)
	for _, f := range files {
		require.Contains(t, files2, f)
	}

	// Remove only the files added by CopyConfiguration
	removed, err := DeleteConfiguration()
	require.NoError(t, err)
	for _, f := range files {
		require.Contains(t, removed, f)
	}
}

func Test_BackupSysctlConfiguration_Integration(t *testing.T) {
	backupFile := path.Join(models.Paths().BackupDir, "sysctl.conf")

	// Record pre-existing backup file and contents
	var originalData []byte
	existed := false
	if _, err := os.Stat(backupFile); err == nil {
		existed = true
		originalData, _ = os.ReadFile(backupFile)
	}

	defer func() {
		if existed {
			_ = os.WriteFile(backupFile, originalData, 0644)
		} else {
			_ = os.Remove(backupFile)
		}
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
	require.GreaterOrEqual(t, len(strings.Split(s, "\n")), 1) // at least one config line
}

func Test_CopySysctlConfigurationFile_Integration(t *testing.T) {
	dst, err := CopyConfigurationFile("75-network-performance.conf")
	require.NoError(t, err)
	require.FileExists(t, dst)

	data, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Contains(t, string(data), "net.ipv4.tcp_keepalive_probes")
	require.NotContains(t, string(data), "fs.file-max", "the old fs.file-max override must not be reintroduced")
}

func Test_ReadBackupValue_Integration(t *testing.T) {
	backupFile := path.Join(t.TempDir(), "sysctl.conf")

	// Missing backup file: not an error, just "not found".
	_, ok, err := ReadBackupValue(backupFile, "fs.file-max")
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, os.WriteFile(backupFile, []byte("fs.file-max = 9223372036854775807\n"), 0644))

	v, ok, err := ReadBackupValue(backupFile, "fs.file-max")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "9223372036854775807", v)

	// A key not present in the backup: "not found", not an error.
	_, ok, err = ReadBackupValue(backupFile, "net.ipv4.tcp_keepalive_probes")
	require.NoError(t, err)
	require.False(t, ok)
}

func Test_PathFromKey(t *testing.T) {
	results, err := PathFromKey("net.ipv4.ip_forward")
	require.NoError(t, err)
	require.Equal(t, []string{"/proc/sys/net/ipv4/ip_forward"}, results)

	// test with wildcard, but it only works if there are matching entries in /proc/sys
	results, err = PathFromKey("net.ipv4.conf.lxc*.rp_filter")
	require.NoError(t, err)
	if len(results) > 0 {
		for _, r := range results {
			require.True(t, strings.HasPrefix(r, "/proc/sys/net/ipv4/conf/lxc"))
			require.True(t, strings.HasSuffix(r, "/rp_filter"))
		}
	}

	results, err = PathFromKey("net.ipv4.conf.invalid*.rp_filter")
	require.NoError(t, err)
	require.Empty(t, results)

	results, err = PathFromKey("-net.ipv4.ip_forward")
	require.NoError(t, err)
	require.Equal(t, []string{"/proc/sys/net/ipv4/ip_forward"}, results)
}
