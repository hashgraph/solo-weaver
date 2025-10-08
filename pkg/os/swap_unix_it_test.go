//go:build linux && integration

package os

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSwapOff_Integration(t *testing.T) {
	f := makeTestSwapFile(t)
	defer func() {
		_ = os.Remove(f.Name())
	}()
	swapFile := f.Name()

	// swapon
	out, err := sudo(exec.Command("/usr/sbin/swapon", swapFile)).CombinedOutput()
	if err != nil {
		t.Fatalf("swapon failed: %v, output: %s", err, out)
	}

	// Now call SwapOff
	err = SwapOff(swapFile)
	if err != nil {
		t.Fatalf("SwapOff failed: %v", err)
	}

	// Confirm swap is off
	out, err = sudo(exec.Command("/usr/sbin/swapon", "--show=NAME")).CombinedOutput()
	if err != nil {
		t.Fatalf("swapon --show failed: %v, output: %s", err, out)
	}
	if strings.Contains(string(out), swapFile) {
		t.Errorf("swap file still active: %s", out)
	}

	// Cleanup: swapoff and remove file
	_ = sudo(exec.Command("/usr/sbin/swapoff", swapFile)).Run()
	_ = os.Remove(swapFile)
}

func TestSwapOn_Integration(t *testing.T) {
	h, err := os.UserHomeDir()
	require.NoError(t, err)

	// swapfile needs to be in ext4 or swapoff fails with "swapoff: /path: Invalid argument"
	f, err := os.CreateTemp(h, "swap-test-")
	require.NoError(t, err)
	defer func() {
		_ = os.Remove(f.Name())
	}()

	swapFile := f.Name()

	out, err := exec.Command("dd", "if=/dev/zero", "of="+swapFile, "bs=1M", "count=16").CombinedOutput()
	require.NoError(t, err, "dd failed: %s", out)

	err = exec.Command("chmod", "0600", swapFile).Run()
	require.NoError(t, err, "chmod failed")

	out, err = sudo(exec.Command("/usr/sbin/mkswap", swapFile)).CombinedOutput()
	require.NoError(t, err, "mkswap failed: %s", out)

	// Call sysSwapOn directly (integration with syscall)
	err = sysSwapOn(swapFile, 0)
	require.NoError(t, err, "sysSwapOn failed")

	// Confirm swap is active
	out, err = sudo(exec.Command("/usr/sbin/swapon", "--show=NAME")).CombinedOutput()
	require.NoError(t, err, "swapon --show failed: %s", out)
	require.Contains(t, string(out), swapFile, "swap file not active")

	// Cleanup: swapoff and remove file
	_ = sudo(exec.Command("/usr/sbin/swapoff", swapFile)).Run()
	_ = os.Remove(swapFile)
}

func TestSwapOnAll_Integration(t *testing.T) {
	f := makeTestSwapFile(t)
	defer func() {
		_ = os.Remove(f.Name())
	}()
	swapFile := f.Name()

	// Write a temporary fstab with the swap file entry
	tmpFstab, err := os.CreateTemp("", "fstab")
	require.NoError(t, err)
	defer func() {
		err := os.Remove(tmpFstab.Name())
		if err != nil {
			t.Errorf("failed to remove temp fstab: %v", err)
		}
	}()
	_, err = tmpFstab.WriteString(swapFile + " none swap sw 0 0\n")
	require.NoError(t, err)
	require.NoError(t, tmpFstab.Close())

	// Patch fstabFile for the test
	fstabFileOrig := fstabFile
	defer func() { fstabFile = fstabFileOrig }()
	fstabFile = tmpFstab.Name()

	// Run SwapOnAll
	err = SwapOnAll()
	require.NoError(t, err)

	// Confirm swap is active
	out, err := exec.Command("/usr/sbin/swapon", "--show=NAME").CombinedOutput()
	require.NoError(t, err, "swapon --show failed: %s", out)
	require.Contains(t, string(out), swapFile, "swap file not active")

	// Cleanup
	_ = exec.Command("/usr/sbin/swapoff", swapFile).Run()
}

func TestSwapOffAll_Integration(t *testing.T) {
	f := makeTestSwapFile(t)
	defer func() {
		_ = os.Remove(f.Name())
	}()
	swapFile := f.Name()

	out, err := exec.Command("/usr/sbin/swapon", swapFile).CombinedOutput()
	require.NoError(t, err, "swapon failed: %s", out)

	// Write a temporary fstab with the swap file entry
	tmpFstab, err := os.CreateTemp("", "fstab")
	require.NoError(t, err)
	defer func() {
		err := os.Remove(tmpFstab.Name())
		if err != nil {
			t.Errorf("failed to remove temp fstab: %v", err)
		}
	}()
	_, err = tmpFstab.WriteString(swapFile + " none swap sw 0 0\n")
	require.NoError(t, err)
	require.NoError(t, tmpFstab.Close())

	// Patch fstabFile for the test
	fstabFileOrig := fstabFile
	defer func() { fstabFile = fstabFileOrig }()
	fstabFile = tmpFstab.Name()

	// Run SwapOffAll
	err = SwapOffAll()
	require.NoError(t, err)

	// Confirm swap is off
	out, err = exec.Command("/usr/sbin/swapon", "--show=NAME").CombinedOutput()
	require.NoError(t, err, "swapon --show failed: %s", out)
	require.NotContains(t, string(out), swapFile, "swap file still active")

	// Cleanup
	_ = exec.Command("/usr/sbin/swapoff", swapFile).Run()
}

func TestDisableSwap_EnableSwap_Integration(t *testing.T) {
	f := makeTestSwapFile(t)
	defer func() {
		_ = os.Remove(f.Name())
	}()
	swapFile := f.Name()

	tmpFstab, err := os.CreateTemp("", "fstab")
	require.NoError(t, err)
	defer func() {
		err := os.Remove(tmpFstab.Name())
		if err != nil {
			t.Errorf("failed to remove temp fstab: %v", err)
		}
	}()

	fstabContent := swapFile + " none swap sw 0 0\n"
	_, err = tmpFstab.WriteString(fstabContent)
	require.NoError(t, err)
	require.NoError(t, tmpFstab.Close())

	// Patch FSTAB_LOCATION for the test
	origFstabLocation := FSTAB_LOCATION
	defer func() { fstabFile = origFstabLocation }()
	fstabFile = tmpFstab.Name()

	// Enable swap (should uncomment, but file is not commented yet)
	err = EnableSwap()
	require.NoError(t, err)

	// Check fstab content is unchanged (no commented lines to uncomment)
	content, err := os.ReadFile(tmpFstab.Name())
	require.NoError(t, err)
	require.Contains(t, string(content), swapFile+" none swap sw 0 0")

	// Disable swap (should comment out swap line)
	err = DisableSwap()
	require.NoError(t, err)

	content, err = os.ReadFile(tmpFstab.Name())
	require.NoError(t, err)
	require.Contains(t, string(content), SwapCommentPrefix+swapFile+" none swap sw 0 0")

	// Enable swap again (should uncomment swap line)
	err = EnableSwap()
	require.NoError(t, err)

	content, err = os.ReadFile(tmpFstab.Name())
	require.NoError(t, err)
	require.Contains(t, string(content), swapFile+" none swap sw 0 0")
}

func TestResolveSpec_Integration(t *testing.T) {
	h, err := os.UserHomeDir()
	require.NoError(t, err)

	tmpDir, err := os.MkdirTemp(h, "resolve-spec-test-")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	byUUIDDir := tmpDir + "/by-uuid"
	byLabelDir := tmpDir + "/by-label"
	require.NoError(t, os.Mkdir(byUUIDDir, 0755))
	require.NoError(t, os.Mkdir(byLabelDir, 0755))

	uuid := "test-uuid-123"
	label := "test-label-abc"
	uuidPath := byUUIDDir + "/" + uuid
	labelPath := byLabelDir + "/" + label
	require.NoError(t, os.WriteFile(uuidPath, []byte{}, 0600))
	require.NoError(t, os.WriteFile(labelPath, []byte{}, 0600))

	// Create dummy device files
	devFile := tmpDir + "/devtest"
	require.NoError(t, os.WriteFile(devFile, []byte{}, 0600))
	devFile2 := tmpDir + "/devtest2"

	require.NoError(t, os.Symlink(devFile, devFile2))

	// Set the lookup path variables for the test
	origUUIDPath := uuidLookupDir
	origLabelPath := labelLookupDir
	uuidLookupDir = byUUIDDir
	labelLookupDir = byLabelDir
	defer func() {
		uuidLookupDir = origUUIDPath
		labelLookupDir = origLabelPath
	}()

	res, err := resolveSpec("UUID=" + uuid)
	require.NoError(t, err)
	require.Equal(t, uuidPath, res)

	res, err = resolveSpec("LABEL=" + label)
	require.NoError(t, err)
	require.Equal(t, labelPath, res)

	res, err = resolveSpec(devFile2)
	require.NoError(t, err)
	require.Equal(t, devFile, res)

	_, err = resolveSpec("UUID=doesnotexist")
	require.Error(t, err)
	_, err = resolveSpec("LABEL=doesnotexist")
	require.Error(t, err)
}
