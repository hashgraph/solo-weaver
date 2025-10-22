//go:build linux && integration

package os

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/stretchr/testify/require"
)

func Test_Swap_SwapOff_Integration(t *testing.T) {
	f := makeTestSwapFile(t)
	swapFile := f.Name()
	t.Cleanup(func() {
		_ = os.Remove(swapFile)
	})

	// swapon
	out, err := sudo(exec.Command("/usr/sbin/swapon", swapFile)).CombinedOutput()
	if err != nil {
		t.Fatalf("swapon failed: %v, output: %s", err, out)
	}
	t.Cleanup(func() {
		_ = sudo(exec.Command("/usr/sbin/swapoff", swapFile)).Run()
	})

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
}

func Test_Swap_SwapOn_Integration(t *testing.T) {
	h, err := os.UserHomeDir()
	require.NoError(t, err)

	// swapfile needs to be in ext4 or swapoff fails with "swapoff: /path: Invalid argument"
	f, err := os.CreateTemp(h, "swap-test-")
	require.NoError(t, err)
	swapFile := f.Name()
	t.Cleanup(func() {
		_ = sudo(exec.Command("/usr/sbin/swapoff", swapFile)).Run()
		_ = os.Remove(swapFile)
	})

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
}

func Test_Swap_SwapOnAll_Integration(t *testing.T) {
	f := makeTestSwapFile(t)
	swapFile := f.Name()
	t.Cleanup(func() {
		_ = exec.Command("/usr/sbin/swapoff", swapFile).Run()
		_ = os.Remove(swapFile)
	})

	// Write a temporary fstab with the swap file entry
	tmpFstab, err := os.CreateTemp("", "fstab")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove(tmpFstab.Name())
	})
	_, err = tmpFstab.WriteString(swapFile + " none swap sw 0 0\n")
	require.NoError(t, err)
	require.NoError(t, tmpFstab.Close())

	// Patch fstabFile for the test
	fstabFileOrig := fstabFile
	t.Cleanup(func() { fstabFile = fstabFileOrig })
	fstabFile = tmpFstab.Name()

	// Run SwapOnAll
	err = SwapOnAll()
	require.NoError(t, err)

	// Confirm swap is active
	out, err := exec.Command("/usr/sbin/swapon", "--show=NAME").CombinedOutput()
	require.NoError(t, err, "swapon --show failed: %s", out)
	require.Contains(t, string(out), swapFile, "swap file not active")
}

func Test_Swap_SwapOffAll_Integration(t *testing.T) {
	f := makeTestSwapFile(t)
	swapFile := f.Name()
	t.Cleanup(func() {
		_ = exec.Command("/usr/sbin/swapoff", swapFile).Run()
		_ = os.Remove(swapFile)
	})

	out, err := exec.Command("/usr/sbin/swapon", swapFile).CombinedOutput()
	require.NoError(t, err, "swapon failed: %s", out)

	// Write a temporary fstab with the swap file entry
	tmpFstab, err := os.CreateTemp("", "fstab")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove(tmpFstab.Name())
	})
	_, err = tmpFstab.WriteString(swapFile + " none swap sw 0 0\n")
	require.NoError(t, err)
	require.NoError(t, tmpFstab.Close())

	// Patch fstabFile for the test
	fstabFileOrig := fstabFile
	t.Cleanup(func() { fstabFile = fstabFileOrig })
	fstabFile = tmpFstab.Name()

	// Run SwapOffAll
	err = SwapOffAll()
	require.NoError(t, err)

	// Confirm swap is off
	out, err = exec.Command("/usr/sbin/swapon", "--show=NAME").CombinedOutput()
	require.NoError(t, err, "swapon --show failed: %s", out)
	require.NotContains(t, string(out), swapFile, "swap file still active")
}

func Test_Swap_DisableSwap_EnableSwap_Integration(t *testing.T) {
	f := makeTestSwapFile(t)
	swapFile := f.Name()

	tmpFstab, err := os.CreateTemp("", "fstab")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = DisableSwap()
		_ = os.Remove(tmpFstab.Name())
		_ = os.Remove(swapFile)
	})

	fstabContent := swapFile + " none swap sw 0 0\n"
	_, err = tmpFstab.WriteString(fstabContent)
	require.NoError(t, err)
	require.NoError(t, tmpFstab.Close())

	// Patch FSTAB_LOCATION for the test
	origFstabLocation := FSTAB_LOCATION
	t.Cleanup(func() { fstabFile = origFstabLocation })
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

func Test_Swap_ResolveSpec_Integration(t *testing.T) {
	h, err := os.UserHomeDir()
	require.NoError(t, err)

	tmpDir, err := os.MkdirTemp(h, "resolve-spec-test-")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.RemoveAll(tmpDir)
	})

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
	t.Cleanup(func() {
		uuidLookupDir = origUUIDPath
		labelLookupDir = origLabelPath
	})

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

func Test_Swap_MaskSwapUnit_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("test requires root privileges")
	}

	// Create a test swap file
	f := makeTestSwapFileForSystemd(t)
	swapFile := f.Name()

	t.Cleanup(func() {
		_ = unmaskSystemdSwapUnits(context.Background())
		_ = sudo(exec.Command("/usr/sbin/swapoff", swapFile)).Run()
		_ = os.Remove(swapFile)
	})

	// Activate swap
	out, err := sudo(exec.Command("/usr/sbin/swapon", swapFile)).CombinedOutput()
	require.NoError(t, err, "swapon failed: %s", out)

	ctx := context.Background()

	// Get active swap units before masking
	unitsBefore, err := getActiveSwapUnits(ctx)
	require.NoError(t, err, "failed to get active swap units")

	// Mask all active swap units
	err = maskSystemdSwapUnits(ctx)
	require.NoError(t, err, "maskSystemdSwapUnits failed")

	// Verify units are masked
	for _, unitName := range unitsBefore {
		masked, err := isUnitMasked(unitName)
		require.NoError(t, err)
		require.True(t, masked, "unit %s should be masked", unitName)
	}
}

func Test_Swap_UnmaskSwapUnit_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("test requires root privileges")
	}

	// Create a test swap file
	f := makeTestSwapFileForSystemd(t)
	swapFile := f.Name()
	t.Cleanup(func() {
		_ = sudo(exec.Command("/usr/sbin/swapoff", swapFile)).Run()
		_ = os.Remove(swapFile)
	})

	// Activate swap
	out, err := sudo(exec.Command("/usr/sbin/swapon", swapFile)).CombinedOutput()
	require.NoError(t, err, "swapon failed: %s", out)

	ctx := context.Background()

	// Get active swap units
	units, err := getActiveSwapUnits(ctx)
	require.NoError(t, err, "failed to get active swap units")
	require.NotEmpty(t, units, "should have at least one active swap unit")

	// First mask them
	err = maskSystemdSwapUnits(ctx)
	require.NoError(t, err, "maskSystemdSwapUnits failed")

	// Verify units are masked
	for _, unitName := range units {
		masked, err := isUnitMasked(unitName)
		require.NoError(t, err)
		require.True(t, masked, "unit %s should be masked", unitName)
	}

	// Now unmask them
	err = unmaskSystemdSwapUnits(ctx)
	require.NoError(t, err, "unmaskSystemdSwapUnits failed")

	// Verify units are no longer masked
	for _, unitName := range units {
		masked, err := isUnitMasked(unitName)
		require.NoError(t, err)
		require.False(t, masked, "unit %s should not be masked", unitName)
	}
}

// isUnitMasked checks if the specified unit is masked.
// It returns true if the unit is masked (symlinked to /dev/null), false otherwise.
func isUnitMasked(name string) (bool, error) {
	// A masked unit has a symlink pointing to /dev/null in /etc/systemd/system
	unitPath := fmt.Sprintf("/etc/systemd/system/%s", name)
	target, err := os.Readlink(unitPath)
	if err != nil {
		// If there's no symlink, it's not masked
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("check if unit %s is masked: %w", name, err)
	}

	return target == "/dev/null", nil
}

// Helper function to create a test swap file
func makeTestSwapFileForSystemd(t *testing.T) *os.File {
	t.Helper()

	h, err := os.UserHomeDir()
	require.NoError(t, err)

	// swapfile needs to be in ext4 or swapoff fails with "swapoff: /path: Invalid argument"
	f, err := os.CreateTemp(h, "swap-systemd-test-")
	require.NoError(t, err)

	swapFile := f.Name()

	out, err := exec.Command("dd", "if=/dev/zero", "of="+swapFile, "bs=1M", "count=16").CombinedOutput()
	require.NoError(t, err, "dd failed: %s", out)

	err = exec.Command("chmod", "0600", swapFile).Run()
	require.NoError(t, err, "chmod failed")

	out, err = sudo(exec.Command("/usr/sbin/mkswap", swapFile)).CombinedOutput()
	require.NoError(t, err, "mkswap failed: %s", out)

	return f
}

// Test_DisableSwap_Integration verifies all 4 steps of DisableSwap:
// 1. Turns off all active swap (swapoff -a)
// 2. Masks systemd swap units to prevent auto-activation
// 3. Comments out swap entries in /etc/fstab
// 4. Reloads systemd daemon
func Test_DisableSwap_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("test requires root privileges")
	}

	// Create a test swap file
	f := makeTestSwapFileForSystemd(t)
	swapFile := f.Name()

	// Create temporary fstab
	tmpFstab, err := os.CreateTemp("", "fstab-disable-")
	require.NoError(t, err)
	fstabContent := swapFile + " none swap sw 0 0\n"
	_, err = tmpFstab.WriteString(fstabContent)
	require.NoError(t, err)
	require.NoError(t, tmpFstab.Close())

	// Backup and patch fstabFile
	origFstabFile := fstabFile
	fstabFile = tmpFstab.Name()

	t.Cleanup(func() {
		// Restore original state
		fstabFile = origFstabFile
		_ = unmaskSystemdSwapUnits(context.Background())
		_ = sudo(exec.Command("/usr/sbin/swapoff", swapFile)).Run()
		_ = os.Remove(tmpFstab.Name())
		_ = os.Remove(swapFile)
	})

	// Activate swap first
	out, err := sudo(exec.Command("/usr/sbin/swapon", swapFile)).CombinedOutput()
	require.NoError(t, err, "swapon failed: %s", out)

	// Verify swap is active before disabling
	activeSwaps, err := parseProcSwaps()
	require.NoError(t, err)
	require.Contains(t, activeSwaps, swapFile, "swap should be active before DisableSwap")

	ctx := context.Background()

	// Get active swap units before disabling
	unitsBefore, err := getActiveSwapUnits(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, unitsBefore, "should have at least one active swap unit")

	// Call DisableSwap
	err = DisableSwap()
	require.NoError(t, err, "DisableSwap should succeed")

	// Step 1 verification: All swap should be turned off
	activeSwaps, err = parseProcSwaps()
	require.NoError(t, err)
	require.NotContains(t, activeSwaps, swapFile, "swap should be off after DisableSwap")

	// Step 2 verification: Systemd swap units should be masked
	for _, unitName := range unitsBefore {
		masked, err := isUnitMasked(unitName)
		require.NoError(t, err)
		require.True(t, masked, "unit %s should be masked after DisableSwap", unitName)
	}

	// Step 3 verification: Swap entries in fstab should be commented out
	content, err := os.ReadFile(tmpFstab.Name())
	require.NoError(t, err)
	require.Contains(t, string(content), SwapCommentPrefix+swapFile+" none swap sw 0 0",
		"swap entry should be commented out in fstab")
	require.NotContains(t, string(content), "\n"+swapFile+" none swap sw 0 0",
		"uncommented swap entry should not exist in fstab")

	// Step 4 verification: Systemd daemon reload is implicit (no error means it succeeded)
	// We can verify by checking that systemd is still responsive
	ctxTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := dbus.NewSystemConnectionContext(ctxTimeout)
	require.NoError(t, err, "systemd should be responsive after daemon-reload")
	conn.Close()
}

// Test_EnableSwap_Integration verifies all 4 steps of EnableSwap:
// 1. Uncomments swap entries in /etc/fstab
// 2. Unmasks systemd swap units to allow auto-activation
// 3. Reloads systemd daemon
// 4. Activates all swap entries from /etc/fstab (swapon -a)
func Test_EnableSwap_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("test requires root privileges")
	}

	// Create a test swap file
	f := makeTestSwapFileForSystemd(t)
	swapFile := f.Name()

	// Create temporary fstab with commented swap entry (simulating disabled state)
	tmpFstab, err := os.CreateTemp("", "fstab-enable-")
	require.NoError(t, err)
	commentedFstabContent := SwapCommentPrefix + swapFile + " none swap sw 0 0\n"
	_, err = tmpFstab.WriteString(commentedFstabContent)
	require.NoError(t, err)
	require.NoError(t, tmpFstab.Close())

	// Backup and patch fstabFile
	origFstabFile := fstabFile
	fstabFile = tmpFstab.Name()

	t.Cleanup(func() {
		// Restore original state
		fstabFile = origFstabFile
		_ = sudo(exec.Command("/usr/sbin/swapoff", swapFile)).Run()
		_ = os.Remove(tmpFstab.Name())
		_ = os.Remove(swapFile)
	})

	// First activate swap and mask it to simulate a disabled state
	out, err := sudo(exec.Command("/usr/sbin/swapon", swapFile)).CombinedOutput()
	require.NoError(t, err, "swapon failed: %s", out)

	ctx := context.Background()

	// Mask the swap units to simulate disabled state
	err = maskSystemdSwapUnits(ctx)
	require.NoError(t, err)

	// Turn off swap to simulate disabled state
	err = SwapOff(swapFile)
	require.NoError(t, err)

	// Get swap units that were masked
	ctxTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := dbus.NewSystemConnectionContext(ctxTimeout)
	require.NoError(t, err)
	defer conn.Close()

	units, err := conn.ListUnitsByPatternsContext(ctxTimeout, []string{}, []string{"*.swap"})
	require.NoError(t, err)
	var maskedUnits []string
	for _, unit := range units {
		if masked, _ := isUnitMasked(unit.Name); masked {
			maskedUnits = append(maskedUnits, unit.Name)
		}
	}

	// Verify initial state: swap is off, units are masked, fstab entries are commented
	activeSwaps, err := parseProcSwaps()
	require.NoError(t, err)
	require.NotContains(t, activeSwaps, swapFile, "swap should be off before EnableSwap")

	content, err := os.ReadFile(tmpFstab.Name())
	require.NoError(t, err)
	require.Contains(t, string(content), SwapCommentPrefix+swapFile,
		"swap entry should be commented before EnableSwap")

	// Call EnableSwap
	err = EnableSwap()
	require.NoError(t, err, "EnableSwap should succeed")

	// Step 1 verification: Swap entries in fstab should be uncommented
	content, err = os.ReadFile(tmpFstab.Name())
	require.NoError(t, err)
	require.Contains(t, string(content), swapFile+" none swap sw 0 0",
		"swap entry should be uncommented in fstab")
	require.NotContains(t, string(content), SwapCommentPrefix+swapFile+" none swap sw 0 0",
		"commented swap entry should not exist in fstab")

	// Step 2 verification: Systemd swap units should be unmasked
	for _, unitName := range maskedUnits {
		masked, err := isUnitMasked(unitName)
		require.NoError(t, err)
		require.False(t, masked, "unit %s should be unmasked after EnableSwap", unitName)
	}

	// Step 3 verification: Systemd daemon reload is implicit (no error means it succeeded)
	// We can verify by checking that systemd is still responsive
	conn2, err := dbus.NewSystemConnectionContext(ctxTimeout)
	require.NoError(t, err, "systemd should be responsive after daemon-reload")
	conn2.Close()

	// Step 4 verification: All swap from fstab should be activated
	activeSwaps, err = parseProcSwaps()
	require.NoError(t, err)
	require.Contains(t, activeSwaps, swapFile, "swap should be active after EnableSwap")
}

// Test_DisableSwap_EnableSwap_RoundTrip_Integration tests the full cycle:
// DisableSwap -> EnableSwap -> DisableSwap to ensure idempotency
func Test_DisableSwap_EnableSwap_RoundTrip_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("test requires root privileges")
	}

	// Create a test swap file
	f := makeTestSwapFileForSystemd(t)
	swapFile := f.Name()

	// Create temporary fstab
	tmpFstab, err := os.CreateTemp("", "fstab-roundtrip-")
	require.NoError(t, err)
	fstabContent := swapFile + " none swap sw 0 0\n"
	_, err = tmpFstab.WriteString(fstabContent)
	require.NoError(t, err)
	require.NoError(t, tmpFstab.Close())

	// Backup and patch fstabFile
	origFstabFile := fstabFile
	fstabFile = tmpFstab.Name()

	t.Cleanup(func() {
		// Restore original state
		fstabFile = origFstabFile
		_ = unmaskSystemdSwapUnits(context.Background())
		_ = sudo(exec.Command("/usr/sbin/swapoff", swapFile)).Run()
		_ = os.Remove(tmpFstab.Name())
		_ = os.Remove(swapFile)
	})

	// Activate swap initially
	out, err := sudo(exec.Command("/usr/sbin/swapon", swapFile)).CombinedOutput()
	require.NoError(t, err, "initial swapon failed: %s", out)

	ctx := context.Background()

	// === First DisableSwap ===
	err = DisableSwap()
	require.NoError(t, err, "first DisableSwap should succeed")

	// Verify swap is disabled
	activeSwaps, err := parseProcSwaps()
	require.NoError(t, err)
	require.NotContains(t, activeSwaps, swapFile, "swap should be off after first DisableSwap")

	content, err := os.ReadFile(tmpFstab.Name())
	require.NoError(t, err)
	require.Contains(t, string(content), SwapCommentPrefix+swapFile,
		"swap entry should be commented after first DisableSwap")

	// === EnableSwap ===
	err = EnableSwap()
	require.NoError(t, err, "EnableSwap should succeed")

	// Verify swap is enabled
	activeSwaps, err = parseProcSwaps()
	require.NoError(t, err)
	require.Contains(t, activeSwaps, swapFile, "swap should be on after EnableSwap")

	content, err = os.ReadFile(tmpFstab.Name())
	require.NoError(t, err)
	require.Contains(t, string(content), swapFile+" none swap sw 0 0",
		"swap entry should be uncommented after EnableSwap")
	require.NotContains(t, string(content), SwapCommentPrefix+swapFile+" none swap sw 0 0",
		"commented swap entry should not exist after EnableSwap")

	// === Second DisableSwap (test idempotency) ===
	err = DisableSwap()
	require.NoError(t, err, "second DisableSwap should succeed")

	// Verify swap is disabled again
	activeSwaps, err = parseProcSwaps()
	require.NoError(t, err)
	require.NotContains(t, activeSwaps, swapFile, "swap should be off after second DisableSwap")

	content, err = os.ReadFile(tmpFstab.Name())
	require.NoError(t, err)
	require.Contains(t, string(content), SwapCommentPrefix+swapFile,
		"swap entry should be commented after second DisableSwap")

	// Verify systemd is still responsive after all operations
	ctxTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := dbus.NewSystemConnectionContext(ctxTimeout)
	require.NoError(t, err, "systemd should be responsive after all operations")
	conn.Close()
}
