//go:build linux

package steps

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/require"
	osx "golang.hedera.com/solo-provisioner/pkg/os"
)

func sudo(cmd *exec.Cmd) *exec.Cmd {
	if os.Geteuid() == 0 {
		return cmd
	}

	// Prepend sudo to the command
	sudoCmd := exec.Command("sudo", append([]string{cmd.Path}, cmd.Args[1:]...)...)
	sudoCmd.Stdout = cmd.Stdout
	sudoCmd.Stderr = cmd.Stderr
	sudoCmd.Stdin = cmd.Stdin

	return sudoCmd
}

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
		err := os.WriteFile(osx.FSTAB_LOCATION, backupFstab, 0644)
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

	// remove the fstabEntry from fstab
	defer func() {
		content, err := os.ReadFile(osx.FSTAB_LOCATION)
		require.NoError(t, err)
		newContent := ""
		scanner := bufio.NewScanner(strings.NewReader(string(content)))
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.Contains(line, swapFile) {
				newContent += line + "\n"
			}
		}
		err = os.WriteFile(osx.FSTAB_LOCATION, []byte(newContent), 0644)
		require.NoError(t, err)
	}()

	// Enable swap
	err = osx.EnableSwap()
	require.NoError(t, err)

	// Run DisableSwap step
	step, err := DisableSwap().Build()
	require.NoError(t, err)
	report, err := step.Execute(context.Background())
	require.NoError(t, err)
	require.NotNil(t, report)
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
