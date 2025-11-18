//go:build integration

package steps

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-weaver/internal/core"
	osx "golang.hedera.com/solo-weaver/pkg/os"
)

func TestDisableSwap_Integration(t *testing.T) {
	// Create a test swap file
	filePattern := "swap-test-*"
	f := makeTestSwapFile(t, filePattern)
	defer func() { _ = os.Remove(f.Name()) }()
	swapFile := f.Name()

	// make a backup of fstab
	backupFstab, err := os.ReadFile(osx.FSTAB_LOCATION)
	require.NoError(t, err)
	defer func() {
		err = os.WriteFile(osx.FSTAB_LOCATION, backupFstab, core.DefaultFilePerm)
		require.NoError(t, err)
		err = sudo(exec.Command("/usr/sbin/swapon", "-a")).Run()
		require.NoError(t, err)
	}()

	// Ensure swap is off
	out, err := sudo(exec.Command("/usr/sbin/swapon", "--show=NAME")).CombinedOutput()
	require.NoError(t, err)
	require.NotContains(t, string(out), filePattern)

	// use sudo to write to /etc/fstab
	fstabEntry := swapFile + " none swap sw 0 0"
	cmd := exec.Command("bash", "-c", fmt.Sprintf("echo '%s' >> %s", fstabEntry, osx.FSTAB_LOCATION))
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, "failed to append to fstab: %s", out)

	// Enable swap
	err = osx.SwapOn(swapFile, 0)
	require.NoError(t, err)

	// Run DisableSwap step
	step, err := DisableSwap().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	// Check fstab content is commented
	content, err := os.ReadFile(osx.FSTAB_LOCATION)
	require.NoError(t, err)
	require.Contains(t, string(content), "#"+swapFile+" none swap sw 0 0")

	// Confirm swap is off
	out, err = sudo(exec.Command("/usr/sbin/swapon", "--show=NAME")).CombinedOutput()
	require.NoError(t, err)
	require.NotContains(t, string(out), swapFile)
}

func makeTestSwapFile(t *testing.T, pattern string) *os.File {
	h, err := os.UserHomeDir()
	require.NoError(t, err)

	// swapfile needs to be in ext4 or swapoff fails with "swapoff: /path: Invalid argument"
	f, err := os.CreateTemp(h, pattern)
	require.NoError(t, err)

	swapFile := f.Name()

	out, err := exec.Command("dd", "if=/dev/zero", "of="+swapFile, "bs=1M", "count=16").CombinedOutput()
	require.NoError(t, err, "dd failed: %s", out)

	err = exec.Command("chmod", "0600", swapFile).Run()
	require.NoError(t, err, "chmod failed")

	out, err = exec.Command("/usr/sbin/mkswap", swapFile).CombinedOutput()
	require.NoError(t, err, "mkswap failed: %s", out)

	return f
}
