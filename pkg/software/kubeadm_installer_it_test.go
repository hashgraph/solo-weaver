//go:build integration

package software

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-weaver/internal/core"
	"golang.hedera.com/solo-weaver/pkg/fsx"
)

func Test_KubeadmInstaller_FullWorkflow_Success(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//
	installer, err := NewKubeadmInstaller()
	require.NoError(t, err, "Failed to create kubeadm installer")

	fileManager, err := fsx.NewManager()
	require.NoError(t, err)

	//
	// When - Download
	//
	err = installer.Download()
	require.NoError(t, err, "Failed to download kubeadm and/or its configuration")

	// Verify downloaded files exist
	_, exists, err := fileManager.PathExists("/opt/solo/weaver/tmp/kubeadm/kubeadm")
	require.NoError(t, err)
	require.True(t, exists, "kubeadm binary should exist in download folder")

	// Check config file exists (10-kubeadm.conf)
	_, exists, err = fileManager.PathExists("/opt/solo/weaver/tmp/kubeadm/10-kubeadm.conf")
	require.NoError(t, err)
	require.True(t, exists, "10-kubeadm.conf should exist in download folder")

	//
	// When - Install
	//
	err = installer.Install()
	require.NoError(t, err, "Failed to install kubeadm")

	// Verify installation files exist in sandbox
	_, exists, err = fileManager.PathExists("/opt/solo/weaver/sandbox/bin/kubeadm")
	require.NoError(t, err)
	require.True(t, exists, "kubeadm binary should exist in sandbox bin directory")

	// Check binary permissions (should be executable)
	info, err := os.Stat("/opt/solo/weaver/sandbox/bin/kubeadm")
	require.NoError(t, err)
	require.Equal(t, os.FileMode(core.DefaultDirOrExecPerm), info.Mode().Perm(), "kubeadm binary should have 0755 permissions")

	// Verify config file is copied to sandbox kubelet service directory
	_, exists, err = fileManager.PathExists("/opt/solo/weaver/sandbox/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf")
	require.NoError(t, err)
	require.True(t, exists, "10-kubeadm.conf should exist in sandbox kubelet service directory")

	//
	// When - Cleanup
	//
	err = installer.Cleanup()
	require.NoError(t, err, "Failed to cleanup kubeadm installation")

	// Check download folder is cleaned up
	_, exists, err = fileManager.PathExists("/opt/solo/weaver/tmp/kubeadm")
	require.NoError(t, err)
	require.False(t, exists, "kubeadm download temp folder should be cleaned up after installation")

	//
	// When - Configure
	//
	err = installer.Configure()
	require.NoError(t, err, "Failed to configure kubeadm")

	// Verify system-wide symlink exists
	_, exists, err = fileManager.PathExists("/usr/local/bin/kubeadm")
	require.NoError(t, err)
	require.True(t, exists, "kubeadm symlink should exist in /usr/local/bin")

	// Verify it's actually a symlink pointing to the sandbox binary
	linkTarget, err := os.Readlink("/usr/local/bin/kubeadm")
	require.NoError(t, err)
	require.Equal(t, "/opt/solo/weaver/sandbox/bin/kubeadm", linkTarget, "symlink should point to sandbox binary")

	// Verify kubelet service directory symlink
	_, exists, err = fileManager.PathExists("/usr/lib/systemd/system/kubelet.service.d")
	require.NoError(t, err)
	require.True(t, exists, "kubelet service directory symlink should exist")

	// Verify it's a symlink pointing to sandbox directory (no 'latest' subfolder)
	linkTarget, err = os.Readlink("/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf")
	require.NoError(t, err)
	require.Equal(t, "/opt/solo/weaver/sandbox/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf", linkTarget, "symlink should point to file in sandbox directory")

	// Verify kubelet path replacement in config file
	configContent, err := os.ReadFile("/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf")
	require.NoError(t, err)
	expectedKubeletPath := filepath.Join(core.Paths().SandboxBinDir, "kubelet")
	require.Contains(t, string(configContent), expectedKubeletPath, "config file should contain updated kubelet path")
	require.NotContains(t, string(configContent), "/usr/bin/kubelet", "config file should not contain old kubelet path")

	// Verify config file permissions
	info, err = os.Stat("/opt/solo/weaver/sandbox/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf")
	require.NoError(t, err)
	require.Equal(t, os.FileMode(core.DefaultFilePerm), info.Mode().Perm(), "config file should have 0644 permissions")
}
