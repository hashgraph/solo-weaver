package steps

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInstallWeaver_CreatesExecutable(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	// Build and execute the installation step
	step, err := InstallWeaver(tmp).Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)

	dest := filepath.Join(tmp, weaverBinaryName)
	info, err := os.Stat(dest)
	require.NoError(t, err, "installed binary should exist")

	require.True(t, info.Mode().IsRegular(), "installed path should be a regular file")
	require.NotZero(t, info.Mode()&0111, "installed binary should be executable")
}

func TestUninstallWeaver_RemovesExecutable(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dest := filepath.Join(tmp, weaverBinaryName)

	// Create a dummy executable file to uninstall
	err := os.WriteFile(dest, []byte("dummy"), 0o755)
	require.NoError(t, err)

	step, err := UninstallWeaver(tmp).Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)

	_, err = os.Stat(dest)
	require.True(t, os.IsNotExist(err), "binary should be removed after uninstall")
}
