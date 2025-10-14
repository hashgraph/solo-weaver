//go:build integration

package software

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
)

func TestKubectlInstaller_FullWorkflow_Success(t *testing.T) {
	setupTestEnvironment(t)

	//
	// Given
	//
	installer, err := NewKubectlInstaller()
	require.NoError(t, err, "Failed to create kubectl installer")

	fileManager, err := fsx.NewManager()
	require.NoError(t, err)

	//
	// When - Download
	//
	err = installer.Download()
	require.NoError(t, err, "Failed to download kubectl and/or its configuration")

	// Verify downloaded files exist
	_, exists, err := fileManager.PathExists("/opt/provisioner/tmp/kubectl/kubectl")
	require.NoError(t, err)
	require.True(t, exists, "kubectl binary should exist in download folder")

	//
	// When - Install
	//
	err = installer.Install()
	require.NoError(t, err, "Failed to install kubectl")

	// Verify installation files exist in sandbox
	_, exists, err = fileManager.PathExists("/opt/provisioner/sandbox/bin/kubectl")
	require.NoError(t, err)
	require.True(t, exists, "kubectl binary should exist in sandbox bin directory")

	// Check binary permissions (should be executable)
	info, err := os.Stat("/opt/provisioner/sandbox/bin/kubectl")
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0755), info.Mode().Perm(), "kubectl binary should have 0755 permissions")

	//
	// When - Configure
	//
	err = installer.Configure()
	require.NoError(t, err, "Failed to configure kubectl")

	// Verify system-wide symlink exists
	_, exists, err = fileManager.PathExists("/usr/local/bin/kubectl")
	require.NoError(t, err)
	require.True(t, exists, "kubectl symlink should exist in /usr/local/bin")

	// Verify it's actually a symlink pointing to the sandbox binary
	linkTarget, err := os.Readlink("/usr/local/bin/kubectl")
	require.NoError(t, err)
	require.Equal(t, "/opt/provisioner/sandbox/bin/kubectl", linkTarget, "symlink should point to sandbox binary")
	
}
