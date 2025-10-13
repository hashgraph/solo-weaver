//go:build integration

package software

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKubeletInstaller_Download_Success(t *testing.T) {
	setupTestEnvironment(t)

	//
	// When
	//
	installer, err := NewKubeletInstaller("")
	require.NoError(t, err, "Failed to create kubelet installer")

	//
	// Given
	//
	err = installer.Download()

	//
	// Then
	//
	require.NoError(t, err, "Failed to download kubelet and/or its configuration")
}

func TestKubeletInstaller_Install_Success(t *testing.T) {
	setupTestEnvironment(t)

	//
	// When
	//
	installer, err := NewKubeletInstaller("")
	require.NoError(t, err, "Failed to create kubelet installer")

	//
	// Given
	//
	err = installer.Download()
	require.NoError(t, err, "Failed to download kubelet and/or its configuration")

	err = installer.Install()

	//
	// Then
	//
	require.NoError(t, err, "Failed to install kubelet")
}
