// SPDX-License-Identifier: Apache-2.0

//go:build integration

package software

import (
	"os"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/testutil"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/stretchr/testify/require"
)

func Test_KubectlInstaller_FullWorkflow_Success(t *testing.T) {
	testutil.ResetTestEnvironment(t)

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

	// Verify downloaded files exist in the shared downloads folder
	_, exists, err := fileManager.PathExists("/opt/solo/weaver/downloads/kubectl")
	require.NoError(t, err)
	require.True(t, exists, "kubectl binary should exist in download folder")

	//
	// When - Install
	//
	err = installer.Install()
	require.NoError(t, err, "Failed to install kubectl")

	// Verify installation files exist in sandbox
	_, exists, err = fileManager.PathExists("/opt/solo/weaver/sandbox/bin/kubectl")
	require.NoError(t, err)
	require.True(t, exists, "kubectl binary should exist in sandbox bin directory")

	// Check binary permissions (should be executable)
	info, err := os.Stat("/opt/solo/weaver/sandbox/bin/kubectl")
	require.NoError(t, err)
	require.Equal(t, os.FileMode(core.DefaultDirOrExecPerm), info.Mode().Perm(), "kubectl binary should have 0755 permissions")

	//
	// When - Cleanup
	//
	err = installer.Cleanup()
	require.NoError(t, err, "Failed to cleanup kubectl installation")

	// Check download folder is cleaned up
	_, exists, err = fileManager.PathExists("/opt/solo/weaver/tmp/kubectl")
	require.NoError(t, err)
	require.False(t, exists, "kubectl download temp folder should be cleaned up after installation")

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
	require.Equal(t, "/opt/solo/weaver/sandbox/bin/kubectl", linkTarget, "symlink should point to sandbox binary")

}
