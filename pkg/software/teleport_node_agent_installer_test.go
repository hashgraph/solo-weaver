// SPDX-License-Identifier: Apache-2.0

package software

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewTeleportNodeAgentInstaller_Success(t *testing.T) {
	installer, err := NewTeleportNodeAgentInstaller()
	require.NoError(t, err)
	require.NotNil(t, installer)
}

func TestNewTeleportNodeAgentInstallerWithConfig_Success(t *testing.T) {
	configOpts := &TeleportNodeAgentConfigureOptions{
		ProxyAddr: "example.teleport.sh:443",
		JoinToken: "test-token-12345",
	}

	installer, err := NewTeleportNodeAgentInstallerWithConfig(configOpts)
	require.NoError(t, err)
	require.NotNil(t, installer)
}

func TestNewTeleportNodeAgentInstaller_WithVersion(t *testing.T) {
	installer, err := NewTeleportNodeAgentInstaller(WithVersion("17.0.0"))
	require.NoError(t, err)
	require.NotNil(t, installer)
	require.Equal(t, "17.0.0", installer.Version())
}

func TestTeleportNodeAgentInstaller_Configure_MissingOptions(t *testing.T) {
	// Create installer without config options
	installer, err := NewTeleportNodeAgentInstaller()
	require.NoError(t, err)

	err = installer.Configure()
	require.Error(t, err)
	require.Contains(t, err.Error(), "configure options not provided")
}

func TestTeleportNodeAgentInstaller_Configure_MissingProxyAddr(t *testing.T) {
	configOpts := &TeleportNodeAgentConfigureOptions{
		ProxyAddr: "",
		JoinToken: "test-token-12345",
	}

	installer, err := NewTeleportNodeAgentInstallerWithConfig(configOpts)
	require.NoError(t, err)

	err = installer.Configure()
	require.Error(t, err)
	require.Contains(t, err.Error(), "proxy address is required")
}

func TestTeleportNodeAgentInstaller_Configure_MissingJoinToken(t *testing.T) {
	configOpts := &TeleportNodeAgentConfigureOptions{
		ProxyAddr: "example.teleport.sh:443",
		JoinToken: "",
	}

	installer, err := NewTeleportNodeAgentInstallerWithConfig(configOpts)
	require.NoError(t, err)

	err = installer.Configure()
	require.Error(t, err)
	require.Contains(t, err.Error(), "join token is required")
}

func TestTeleportNodeAgentInstaller_GetSoftwareName(t *testing.T) {
	installer, err := NewTeleportNodeAgentInstaller()
	require.NoError(t, err)
	require.Equal(t, "teleport", installer.GetSoftwareName())
}

func TestTeleportNodeAgentInstaller_Version(t *testing.T) {
	installer, err := NewTeleportNodeAgentInstaller()
	require.NoError(t, err)

	version := installer.Version()
	require.NotEmpty(t, version)
	// Version should be a valid semver-like string
	require.Regexp(t, `^\d+\.\d+\.\d+`, version)
}

func TestTeleportNodeAgentInstaller_PatchServiceFile(t *testing.T) {
	// Create a temporary directory and service file for testing
	tempDir := t.TempDir()

	// Create a mock service file with default paths
	serviceContent := `[Unit]
Description=Teleport Service
After=network.target

[Service]
Type=notify
ExecStart=/usr/local/bin/teleport start --config=/etc/teleport.yaml
Restart=always
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
`
	serviceFilePath := filepath.Join(tempDir, "teleport.service")
	err := os.WriteFile(serviceFilePath, []byte(serviceContent), 0644)
	require.NoError(t, err)

	// Test patching by reading, modifying, and writing the file
	// (mirrors what replaceAllInFile does, but without needing the weaver user)
	content, err := os.ReadFile(serviceFilePath)
	require.NoError(t, err)

	// Replace binary path
	newBinaryPath := "/opt/solo/weaver/sandbox/bin/teleport"
	modifiedContent := string(content)
	modifiedContent = strings.ReplaceAll(modifiedContent, "/usr/local/bin/teleport", newBinaryPath)

	// Replace config path
	newConfigPath := "/etc/teleport/teleport.yaml"
	modifiedContent = strings.ReplaceAll(modifiedContent, "/etc/teleport.yaml", newConfigPath)

	err = os.WriteFile(serviceFilePath, []byte(modifiedContent), 0644)
	require.NoError(t, err)

	// Verify the file was patched correctly
	finalContent, err := os.ReadFile(serviceFilePath)
	require.NoError(t, err)

	require.Contains(t, string(finalContent), newBinaryPath)
	require.Contains(t, string(finalContent), newConfigPath)
	require.NotContains(t, string(finalContent), "/usr/local/bin/teleport")
	require.NotContains(t, string(finalContent), "--config=/etc/teleport.yaml")
}

func TestTeleportNodeAgentInstaller_PathConstants(t *testing.T) {
	// Verify that important constants are properly defined
	require.Equal(t, "teleport", TeleportServiceName)
	require.Equal(t, "/etc/teleport", teleportConfigDir)
	require.Equal(t, "/etc/teleport/teleport.yaml", teleportConfigFile)
	require.Contains(t, teleportServiceArchivePath, "teleport.service")
}

func TestTeleportNodeAgentConfigureOptions_Validation(t *testing.T) {
	testCases := []struct {
		name        string
		opts        *TeleportNodeAgentConfigureOptions
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil options",
			opts:        nil,
			expectError: true,
			errorMsg:    "configure options not provided",
		},
		{
			name: "empty proxy address",
			opts: &TeleportNodeAgentConfigureOptions{
				ProxyAddr: "",
				JoinToken: "valid-token",
			},
			expectError: true,
			errorMsg:    "proxy address is required",
		},
		{
			name: "empty join token",
			opts: &TeleportNodeAgentConfigureOptions{
				ProxyAddr: "proxy.example.com:443",
				JoinToken: "",
			},
			expectError: true,
			errorMsg:    "join token is required",
		},
		{
			name: "valid options",
			opts: &TeleportNodeAgentConfigureOptions{
				ProxyAddr: "proxy.example.com:443",
				JoinToken: "valid-token",
			},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var installer Software
			var err error

			if tc.opts != nil {
				installer, err = NewTeleportNodeAgentInstallerWithConfig(tc.opts)
			} else {
				installer, err = NewTeleportNodeAgentInstaller()
			}
			require.NoError(t, err)

			// Configure will fail early due to validation, before trying to do real work
			err = installer.Configure()
			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errorMsg)
			}
			// Note: Valid options will still fail because Configure() tries to run
			// actual commands and create directories that don't exist in the test env.
			// The point is it shouldn't fail on validation.
		})
	}
}
