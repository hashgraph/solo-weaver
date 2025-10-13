//go:build integration

package software

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
)

func TestKubeadmInstaller_FullWorkflow_Success(t *testing.T) {
	setupTestEnvironment(t)

	//
	// Given
	//
	installer, err := NewKubeadmInstaller("")
	require.NoError(t, err, "Failed to create kubeadm installer")

	fileManager, err := fsx.NewManager()
	require.NoError(t, err)

	//
	// When - Download
	//
	err = installer.Download()
	require.NoError(t, err, "Failed to download kubeadm and/or its configuration")

	// Verify downloaded files exist
	_, exists, err := fileManager.PathExists("/opt/provisioner/tmp/kubeadm/kubeadm")
	require.NoError(t, err)
	require.True(t, exists, "kubeadm binary should exist in download folder")

	// Check config file exists (10-kubeadm.conf)
	_, exists, err = fileManager.PathExists("/opt/provisioner/tmp/kubeadm/10-kubeadm.conf")
	require.NoError(t, err)
	require.True(t, exists, "10-kubeadm.conf should exist in download folder")

	//
	// When - Install
	//
	err = installer.Install()
	require.NoError(t, err, "Failed to install kubeadm")

	// Verify installation files exist in sandbox
	_, exists, err = fileManager.PathExists("/opt/provisioner/sandbox/bin/kubeadm")
	require.NoError(t, err)
	require.True(t, exists, "kubeadm binary should exist in sandbox bin directory")

	// Check binary permissions (should be executable)
	info, err := os.Stat("/opt/provisioner/sandbox/bin/kubeadm")
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0755), info.Mode().Perm(), "kubeadm binary should have 0755 permissions")

	// Verify config file is copied to sandbox kubelet service directory
	_, exists, err = fileManager.PathExists("/opt/provisioner/sandbox/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf")
	require.NoError(t, err)
	require.True(t, exists, "10-kubeadm.conf should exist in sandbox kubelet service directory")

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
	require.Equal(t, "/opt/provisioner/sandbox/bin/kubeadm", linkTarget, "symlink should point to sandbox binary")

	// Verify kubelet service directory symlink
	_, exists, err = fileManager.PathExists("/usr/lib/systemd/system/kubelet.service.d")
	require.NoError(t, err)
	require.True(t, exists, "kubelet service directory symlink should exist")

	// Verify it's a symlink pointing to sandbox directory
	linkTarget, err = os.Readlink("/usr/lib/systemd/system/kubelet.service.d")
	require.NoError(t, err)
	require.Equal(t, "/opt/provisioner/sandbox/usr/lib/systemd/system/kubelet.service.d", linkTarget, "kubelet service directory symlink should point to sandbox directory")

	// Verify kubelet path replacement in config file
	configContent, err := os.ReadFile("/opt/provisioner/sandbox/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf")
	require.NoError(t, err)
	expectedKubeletPath := filepath.Join(core.Paths().SandboxBinDir, "kubelet")
	require.Contains(t, string(configContent), expectedKubeletPath, "config file should contain updated kubelet path")
	require.NotContains(t, string(configContent), "/usr/bin/kubelet", "config file should not contain old kubelet path")

	// Verify config file permissions
	info, err = os.Stat("/opt/provisioner/sandbox/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf")
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0644), info.Mode().Perm(), "config file should have 0644 permissions")
}

func TestReplaceKubeletPath(t *testing.T) {
	fsxManager, err := fsx.NewManager()
	require.NoError(t, err)

	ki := kubeadmInstaller{
		BaseInstaller: &BaseInstaller{
			fileManager: fsxManager,
		},
	}

	// Create a temp dir and file
	tmpDir := t.TempDir()
	origPath := filepath.Join(tmpDir, "10-kubeadm.conf")
	origContent := "ExecStart=/usr/bin/kubelet $KUBELET_KUBEADM_ARGS\n"
	if err := os.WriteFile(origPath, []byte(origContent), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	newKubeletPath := "/custom/bin/kubelet"
	if err := ki.replaceKubeletPath(origPath, newKubeletPath); err != nil {
		t.Fatalf("replaceKubeletPath failed: %v", err)
	}

	// Read back and check
	updated, err := os.ReadFile(origPath)
	if err != nil {
		t.Fatalf("failed to read updated file: %v", err)
	}
	if !strings.Contains(string(updated), newKubeletPath) {
		t.Errorf("expected file to contain new kubelet path %q, got %q", newKubeletPath, string(updated))
	}
	if strings.Contains(string(updated), "/usr/bin/kubelet") {
		t.Errorf("old kubelet path still present in file")
	}
}
