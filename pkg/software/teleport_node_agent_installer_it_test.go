// SPDX-License-Identifier: Apache-2.0

//go:build integration

package software

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/testutil"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/stretchr/testify/require"
)

// Test_TeleportNodeAgentInstaller_DownloadExtractInstall tests the full workflow up to Install
// Note: Configure is skipped because it requires a running Teleport server and valid join token.
// For full end-to-end testing with Configure, see test/teleport/README.md for local dev setup.
func Test_TeleportNodeAgentInstaller_DownloadExtractInstall(t *testing.T) {
	testutil.ResetTestEnvironment(t)

	//
	// Given
	//
	installer, err := NewTeleportNodeAgentInstaller()
	require.NoError(t, err, "Failed to create teleport installer")

	fileManager, err := fsx.NewManager()
	require.NoError(t, err)

	//
	// When - Download
	//
	err = installer.Download()
	require.NoError(t, err, "Failed to download teleport archive")

	// Verify downloaded archive exists
	archiveName := "teleport-ent-v" + installer.Version() + "-linux-" + getTestArch() + "-bin.tar.gz"
	downloadPath := filepath.Join(core.Paths().DownloadsDir, archiveName)
	_, exists, err := fileManager.PathExists(downloadPath)
	require.NoError(t, err)
	require.True(t, exists, "teleport archive should exist in download folder at %s", downloadPath)

	//
	// When - Extract
	//
	err = installer.Extract()
	require.NoError(t, err, "Failed to extract teleport archive")

	// Verify extracted binaries exist
	extractFolder := filepath.Join(core.Paths().TempDir, "teleport", "unpack", "teleport-ent")
	_, exists, err = fileManager.PathExists(filepath.Join(extractFolder, "teleport"))
	require.NoError(t, err)
	require.True(t, exists, "teleport binary should exist in extract folder")

	_, exists, err = fileManager.PathExists(filepath.Join(extractFolder, "tctl"))
	require.NoError(t, err)
	require.True(t, exists, "tctl binary should exist in extract folder")

	_, exists, err = fileManager.PathExists(filepath.Join(extractFolder, "tsh"))
	require.NoError(t, err)
	require.True(t, exists, "tsh binary should exist in extract folder")

	//
	// When - Install
	//
	err = installer.Install()
	require.NoError(t, err, "Failed to install teleport")

	// Verify binaries exist in sandbox
	_, exists, err = fileManager.PathExists(filepath.Join(core.Paths().SandboxBinDir, "teleport"))
	require.NoError(t, err)
	require.True(t, exists, "teleport binary should exist in sandbox bin directory")

	_, exists, err = fileManager.PathExists(filepath.Join(core.Paths().SandboxBinDir, "tctl"))
	require.NoError(t, err)
	require.True(t, exists, "tctl binary should exist in sandbox bin directory")

	_, exists, err = fileManager.PathExists(filepath.Join(core.Paths().SandboxBinDir, "tsh"))
	require.NoError(t, err)
	require.True(t, exists, "tsh binary should exist in sandbox bin directory")

	// Verify teleport.service exists in sandbox systemd directory
	sandboxServicePath := filepath.Join(core.Paths().SandboxDir, core.SystemdUnitFilesDir, "teleport.service")
	_, exists, err = fileManager.PathExists(sandboxServicePath)
	require.NoError(t, err)
	require.True(t, exists, "teleport.service should exist in sandbox systemd directory at %s", sandboxServicePath)

	// Verify binary permissions (should be executable)
	info, err := os.Stat(filepath.Join(core.Paths().SandboxBinDir, "teleport"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(core.DefaultDirOrExecPerm), info.Mode().Perm(), "teleport binary should have 0755 permissions")

	// Verify IsInstalled returns true
	isInstalled, err := installer.IsInstalled()
	require.NoError(t, err)
	require.True(t, isInstalled, "teleport should be marked as installed")

	//
	// When - Cleanup
	//
	err = installer.Cleanup()
	require.NoError(t, err, "Failed to cleanup teleport installation")

	// Check extract folder is cleaned up (temp folder)
	_, exists, err = fileManager.PathExists(filepath.Join(core.Paths().TempDir, "teleport"))
	require.NoError(t, err)
	require.False(t, exists, "teleport temp folder should be cleaned up after installation")
}

// Test_TeleportNodeAgentInstaller_Uninstall tests the uninstall workflow
func Test_TeleportNodeAgentInstaller_Uninstall(t *testing.T) {
	testutil.ResetTestEnvironment(t)

	//
	// Given - Install teleport first
	//
	installer, err := NewTeleportNodeAgentInstaller()
	require.NoError(t, err)

	fileManager, err := fsx.NewManager()
	require.NoError(t, err)

	// Download, Extract, and Install
	err = installer.Download()
	require.NoError(t, err)

	err = installer.Extract()
	require.NoError(t, err)

	err = installer.Install()
	require.NoError(t, err)

	// Verify installed
	isInstalled, err := installer.IsInstalled()
	require.NoError(t, err)
	require.True(t, isInstalled)

	//
	// When - Uninstall
	//
	err = installer.Uninstall()
	require.NoError(t, err, "Failed to uninstall teleport")

	// Verify binaries are removed from sandbox
	_, exists, err := fileManager.PathExists(filepath.Join(core.Paths().SandboxBinDir, "teleport"))
	require.NoError(t, err)
	require.False(t, exists, "teleport binary should be removed from sandbox")

	_, exists, err = fileManager.PathExists(filepath.Join(core.Paths().SandboxBinDir, "tctl"))
	require.NoError(t, err)
	require.False(t, exists, "tctl binary should be removed from sandbox")

	_, exists, err = fileManager.PathExists(filepath.Join(core.Paths().SandboxBinDir, "tsh"))
	require.NoError(t, err)
	require.False(t, exists, "tsh binary should be removed from sandbox")

	// Verify IsInstalled returns false
	isInstalled, err = installer.IsInstalled()
	require.NoError(t, err)
	require.False(t, isInstalled, "teleport should not be marked as installed after uninstall")
}

// Test_TeleportNodeAgentInstaller_ServiceFilePatching tests that the service file is correctly patched
func Test_TeleportNodeAgentInstaller_ServiceFilePatching(t *testing.T) {
	testutil.ResetTestEnvironment(t)

	//
	// Given - Install teleport
	//
	installer, err := NewTeleportNodeAgentInstaller()
	require.NoError(t, err)

	err = installer.Download()
	require.NoError(t, err)

	err = installer.Extract()
	require.NoError(t, err)

	err = installer.Install()
	require.NoError(t, err)

	//
	// When - Read the installed service file
	//
	sandboxServicePath := filepath.Join(core.Paths().SandboxDir, core.SystemdUnitFilesDir, "teleport.service")
	content, err := os.ReadFile(sandboxServicePath)
	require.NoError(t, err)

	//
	// Then - The service file should NOT be patched yet (patching happens in Configure)
	// At Install time, the service file is just copied from the archive
	//
	serviceContent := string(content)

	// The file should exist and contain systemd directives
	require.Contains(t, serviceContent, "[Unit]")
	require.Contains(t, serviceContent, "[Service]")
	require.Contains(t, serviceContent, "[Install]")
}

// Test_TeleportNodeAgentInstaller_VersionSelection tests version selection
func Test_TeleportNodeAgentInstaller_VersionSelection(t *testing.T) {
	testCases := []struct {
		name            string
		requestedVer    string
		expectLatest    bool
		expectSpecified bool
	}{
		{
			name:         "default uses latest version",
			expectLatest: true,
		},
		{
			name:            "explicit version is used",
			requestedVer:    "17.0.0",
			expectSpecified: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var installer Software
			var err error

			if tc.requestedVer != "" {
				installer, err = NewTeleportNodeAgentInstaller(WithVersion(tc.requestedVer))
			} else {
				installer, err = NewTeleportNodeAgentInstaller()
			}
			require.NoError(t, err)

			version := installer.Version()
			require.NotEmpty(t, version)

			if tc.expectSpecified {
				require.Equal(t, tc.requestedVer, version)
			}

			if tc.expectLatest {
				// Should be a valid semver-like version
				require.Regexp(t, `^\d+\.\d+\.\d+`, version)
			}
		})
	}
}

// Test_TeleportNodeAgentInstaller_StateManagement tests state recording
func Test_TeleportNodeAgentInstaller_StateManagement(t *testing.T) {
	testutil.ResetTestEnvironment(t)

	//
	// Given
	//
	installer, err := NewTeleportNodeAgentInstaller()
	require.NoError(t, err)

	stateManager := installer.GetStateManager()
	require.NotNil(t, stateManager)

	//
	// When - Install
	//
	err = installer.Download()
	require.NoError(t, err)

	err = installer.Extract()
	require.NoError(t, err)

	err = installer.Install()
	require.NoError(t, err)

	//
	// Then - State should be recorded
	//
	isInstalled, err := installer.IsInstalled()
	require.NoError(t, err)
	require.True(t, isInstalled)

	//
	// When - Uninstall
	//
	err = installer.Uninstall()
	require.NoError(t, err)

	//
	// Then - State should be removed
	//
	isInstalled, err = installer.IsInstalled()
	require.NoError(t, err)
	require.False(t, isInstalled)
}

// getTestArch returns the current architecture for test assertions
func getTestArch() string {
	arch := os.Getenv("GOARCH")
	if arch == "" {
		arch = "amd64" // Default for most CI environments
	}
	// Map Go arch names to teleport archive naming convention
	switch arch {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	default:
		return arch
	}
}
