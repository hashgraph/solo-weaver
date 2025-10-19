//go:build integration

package software

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
)

func TestKubeletInstaller_FullWorkflow_Success(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//
	installer, err := NewKubeletInstaller()
	require.NoError(t, err, "Failed to create kubelet installer")

	fileManager, err := fsx.NewManager()
	require.NoError(t, err)

	//
	// When - Download
	//
	err = installer.Download()
	require.NoError(t, err, "Failed to download kubelet and/or its configuration")

	// Verify downloaded files exist
	_, exists, err := fileManager.PathExists("/opt/provisioner/tmp/kubelet/kubelet")
	require.NoError(t, err)
	require.True(t, exists, "kubelet binary should exist in download folder")

	// Check config file exists (kubelet.service)
	_, exists, err = fileManager.PathExists("/opt/provisioner/tmp/kubelet/kubelet.service")
	require.NoError(t, err)
	require.True(t, exists, "kubelet.service should exist in download folder")

	//
	// When - Install
	//
	err = installer.Install()
	require.NoError(t, err, "Failed to install kubelet")

	// Verify installation files exist in sandbox
	_, exists, err = fileManager.PathExists("/opt/provisioner/sandbox/bin/kubelet")
	require.NoError(t, err)
	require.True(t, exists, "kubelet binary should exist in sandbox bin directory")

	// Check binary permissions (should be executable)
	info, err := os.Stat("/opt/provisioner/sandbox/bin/kubelet")
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0755), info.Mode().Perm(), "kubelet binary should have 0755 permissions")

	// Verify config file is copied to sandbox kubelet service directory
	_, exists, err = fileManager.PathExists("/opt/provisioner/sandbox/usr/lib/systemd/system/kubelet.service")
	require.NoError(t, err)
	require.True(t, exists, "kubelet.service should exist in sandbox kubelet service directory")

	//
	// When - Configure
	//
	err = installer.Configure()
	require.NoError(t, err, "Failed to configure kubelet")

	// Verify system-wide symlink exists
	_, exists, err = fileManager.PathExists("/usr/local/bin/kubelet")
	require.NoError(t, err)
	require.True(t, exists, "kubelet symlink should exist in /usr/local/bin")

	// Verify it's actually a symlink pointing to the sandbox binary
	linkTarget, err := os.Readlink("/usr/local/bin/kubelet")
	require.NoError(t, err)
	require.Equal(t, "/opt/provisioner/sandbox/bin/kubelet", linkTarget, "symlink should point to sandbox binary")

	// Verify kubelet service directory symlink
	_, exists, err = fileManager.PathExists("/usr/lib/systemd/system/kubelet.service")
	require.NoError(t, err)
	require.True(t, exists, "kubelet service symlink should exist")

	// Verify it's a symlink pointing to sandbox directory
	linkTarget, err = os.Readlink("/usr/lib/systemd/system/kubelet.service")
	require.NoError(t, err)
	require.Equal(t, "/opt/provisioner/sandbox/usr/lib/systemd/system/kubelet.service", linkTarget, "kubelet service directory symlink should point to sandbox directory")

	// Verify kubelet path replacement in config file
	configContent, err := os.ReadFile("/opt/provisioner/sandbox/usr/lib/systemd/system/kubelet.service")
	require.NoError(t, err)
	expectedKubeletPath := filepath.Join(core.Paths().SandboxBinDir, "kubelet")
	require.Contains(t, string(configContent), expectedKubeletPath, "file should contain updated kubelet path")
	require.NotContains(t, string(configContent), "/usr/bin/kubelet", "config file should not contain old kubelet path")

	// Verify config file permissions
	info, err = os.Stat("/opt/provisioner/sandbox/usr/lib/systemd/system/kubelet.service")
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0644), info.Mode().Perm(), "config file should have 0644 permissions")
}
