package os

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

func TestParseProcSwaps_Normal(t *testing.T) {
	tmp, err := os.CreateTemp("", "swaps")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())
	swapsFileOrig := swapsFile
	defer func() { swapsFile = swapsFileOrig }()
	swapsFile = tmp.Name()

	content := "Filename\tType\tSize\tUsed\tPriority\n/dev/sda1 partition 12345 0 -2\n/swapfile file 12345 0 -3\n"
	if _, err := tmp.WriteString(content); err != nil {
		t.Fatal(err)
	}
	tmp.Close()

	swaps, err := parseProcSwaps()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(swaps) != 2 || swaps[0] != "/dev/sda1" || swaps[1] != "/swapfile" {
		t.Errorf("unexpected swaps: %v", swaps)
	}
}

func TestParseProcSwaps_EmptyFile(t *testing.T) {
	tmp, err := os.CreateTemp("", "swaps")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())
	swapsFileOrig := swapsFile
	defer func() { swapsFile = swapsFileOrig }()
	swapsFile = tmp.Name()

	tmp.Close()
	swaps, err := parseProcSwaps()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(swaps) != 0 {
		t.Errorf("expected empty swaps, got: %v", swaps)
	}
}

func TestParseProcSwaps_FileNotFound(t *testing.T) {
	swapsFileOrig := swapsFile
	defer func() { swapsFile = swapsFileOrig }()
	swapsFile = "/tmp/nonexistent_swaps_file"

	_, err := parseProcSwaps()
	if err == nil {
		t.Errorf("expected error for missing file, got nil")
	}
}

func TestParseFstabSwaps_Normal(t *testing.T) {
	tmp, err := os.CreateTemp("", "fstab")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())
	fstabFileOrig := fstabFile
	defer func() { fstabFile = fstabFileOrig }()
	fstabFile = tmp.Name()

	content := `
# comment line
/dev/sda1   none   swap   sw   0   0
/dev/sda2   /mnt   ext4   defaults   0   0
/swapfile   none   swap   sw   0   0
`
	if _, err := tmp.WriteString(content); err != nil {
		t.Fatal(err)
	}
	tmp.Close()

	swaps, err := parseFstabSwaps()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(swaps) != 2 || swaps[0] != "/dev/sda1" || swaps[1] != "/swapfile" {
		t.Errorf("unexpected swaps: %v", swaps)
	}
}

func TestParseFstabSwaps_EmptyFile(t *testing.T) {
	tmp, err := os.CreateTemp("", "fstab")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())
	fstabFileOrig := fstabFile
	defer func() { fstabFile = fstabFileOrig }()
	fstabFile = tmp.Name()

	tmp.Close()
	swaps, err := parseFstabSwaps()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(swaps) != 0 {
		t.Errorf("expected empty swaps, got: %v", swaps)
	}
}

func TestParseFstabSwaps_FileNotFound(t *testing.T) {
	fstabFileOrig := fstabFile
	defer func() { fstabFile = fstabFileOrig }()
	fstabFile = "/tmp/nonexistent_fstab_file"

	swaps, err := parseFstabSwaps()
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if len(swaps) != 0 {
		t.Errorf("expected empty swaps, got: %v", swaps)
	}
}

func TestParseFstabSwaps_CommentAndShortLines(t *testing.T) {
	tmp, err := os.CreateTemp("", "fstab")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())
	fstabFileOrig := fstabFile
	defer func() { fstabFile = fstabFileOrig }()
	fstabFile = tmp.Name()

	content := `
# just a comment
not-enough-fields
/dev/sda1 none swap sw 0 0
`
	if _, err := tmp.WriteString(content); err != nil {
		t.Fatal(err)
	}
	tmp.Close()

	swaps, err := parseFstabSwaps()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(swaps) != 1 || swaps[0] != "/dev/sda1" {
		t.Errorf("unexpected swaps: %v", swaps)
	}
}

func TestIsActiveSwap(t *testing.T) {
	tests := []struct {
		spec        string
		activeSwaps []string
		want        bool
	}{
		{"/dev/sda1", []string{"/dev/sda1", "/swapfile"}, true},
		{"/swapfile", []string{"/dev/sda1", "/swapfile"}, true},
		{"/dev/sdb1", []string{"/dev/sda1", "/swapfile"}, false},
		{"", []string{}, false},
		{"/dev/sda1", []string{}, false},
	}

	for _, tt := range tests {
		got := isActiveSwap(tt.spec, tt.activeSwaps)
		if got != tt.want {
			t.Errorf("isActiveSwap(%q, %v) = %v, want %v", tt.spec, tt.activeSwaps, got, tt.want)
		}
	}
}

func TestSwapOff(t *testing.T) {
	origSysSwapOff := sysSwapOff
	defer func() { sysSwapOff = origSysSwapOff }()

	tests := []struct {
		name      string
		mockErr   error
		wantCode  int
		wantInErr string
	}{
		{"Success", nil, SWAPOFF_EX_OK, ""},
		{"EPERM", syscall.EPERM, SWAPOFF_EX_USAGE, "not super user"},
		{"ENOMEM", syscall.ENOMEM, SWAPOFF_EX_ENOMEM, "cannot allocate memory"},
		{"UnknownSyscall", syscall.Errno(123), SWAPOFF_EX_FAILURE, "unknown syscall error"},
		{"NonSyscall", fmt.Errorf("other error"), SWAPOFF_EX_FAILURE, "non syscall error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sysSwapOff = func(path string) error { return tt.mockErr }
			code, err := SwapOff("/dev/swap")
			if code != tt.wantCode {
				t.Errorf("got code %d, want %d", code, tt.wantCode)
			}
			if tt.wantInErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantInErr) {
					t.Errorf("expected error containing %q, got %v", tt.wantInErr, err)
				}
			} else if err != nil {
				t.Errorf("expected nil error, got %v", err)
			}
		})
	}
}

func TestSwapOn(t *testing.T) {
	origSysSwapOn := sysSwapOn
	defer func() { sysSwapOn = origSysSwapOn }()

	tests := []struct {
		name      string
		mockErr   error
		wantCode  int
		wantInErr string
	}{
		{"Success", nil, SWAPOFF_EX_OK, ""},
		{"EPERM", syscall.EPERM, SWAPOFF_EX_USAGE, "not super user"},
		{"UnknownSyscall", syscall.Errno(123), SWAPOFF_EX_FAILURE, "unknown syscall error"},
		{"NonSyscall", fmt.Errorf("other error"), SWAPOFF_EX_FAILURE, "non syscall error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sysSwapOn = func(path string, flags uintptr) error { return tt.mockErr }
			code, err := SwapOn("/dev/swap", 0)
			if code != tt.wantCode {
				t.Errorf("got code %d, want %d", code, tt.wantCode)
			}
			if tt.wantInErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantInErr) {
					t.Errorf("expected error containing %q, got %v", tt.wantInErr, err)
				}
			} else if err != nil {
				t.Errorf("expected nil error, got %v", err)
			}
		})
	}
}

func TestSwapOff_Integration(t *testing.T) {
	h, err := os.UserHomeDir()
	require.NoError(t, err)

	// swapfile needs to be in ext4 or swapoff fails with "swapoff: /path: Invalid argument"
	f, err := os.CreateTemp(h, "swap-test-")
	require.NoError(t, err)
	defer func() {
		_ = os.Remove(f.Name())
	}()

	swapFile := f.Name()

	out, err := exec.Command("dd", "if=/dev/zero", fmt.Sprintf("of=%s", swapFile), "bs=1M", "count=16").CombinedOutput()
	if err != nil {
		t.Fatalf("dd failed: %v, output: %s", err, out)
	}

	err = exec.Command("chmod", "0600", swapFile).Run()
	if err != nil {
		t.Fatalf("chmod failed: %v", err)
	}

	// mkswap
	if out, err := exec.Command("mkswap", swapFile).CombinedOutput(); err != nil {
		t.Fatalf("mkswap failed: %v, output: %s", err, out)
	}

	// swapon
	out, err = exec.Command("swapon", swapFile).CombinedOutput()
	if err != nil {
		t.Fatalf("swapon failed: %v, output: %s", err, out)
	}

	// Now call SwapOff
	code, err := SwapOff(swapFile)
	if code != SWAPOFF_EX_OK || err != nil {
		t.Fatalf("SwapOff failed: code=%d, err=%v", code, err)
	}

	// Confirm swap is off
	out, err = exec.Command("swapon", "--show=NAME").CombinedOutput()
	if err != nil {
		t.Fatalf("swapon --show failed: %v, output: %s", err, out)
	}
	if string(out) != "NAME\n" && !os.IsNotExist(err) {
		if string(out) != "" && !os.IsNotExist(err) && string(out) != "NAME\n" {
			t.Errorf("swap file still active: %s", out)
		}
	}

	// Cleanup: swapoff and remove file
	_ = exec.Command("swapoff", swapFile).Run()
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

	out, err = exec.Command("mkswap", swapFile).CombinedOutput()
	require.NoError(t, err, "mkswap failed: %s", out)

	// Call sysSwapOn directly (integration with syscall)
	err = sysSwapOn(swapFile, 0)
	require.NoError(t, err, "sysSwapOn failed")

	// Confirm swap is active
	out, err = exec.Command("swapon", "--show=NAME").CombinedOutput()
	require.NoError(t, err, "swapon --show failed: %s", out)
	require.Contains(t, string(out), swapFile, "swap file not active")

	// Cleanup: swapoff and remove file
	_ = exec.Command("swapoff", swapFile).Run()
	_ = os.Remove(swapFile)
}

func TestSwapOnAll_Integration(t *testing.T) {
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

	out, err = exec.Command("mkswap", swapFile).CombinedOutput()
	require.NoError(t, err, "mkswap failed: %s", out)

	// Write a temporary fstab with the swap file entry
	tmpFstab, err := os.CreateTemp("", "fstab")
	require.NoError(t, err)
	defer os.Remove(tmpFstab.Name())
	_, err = tmpFstab.WriteString(swapFile + " none swap sw 0 0\n")
	require.NoError(t, err)
	tmpFstab.Close()

	// Patch fstabFile for the test
	fstabFileOrig := fstabFile
	defer func() { fstabFile = fstabFileOrig }()
	fstabFile = tmpFstab.Name()

	// Run SwapOnAll
	code, err := SwapOnAll()
	require.Equal(t, SWAPOFF_EX_OK, code, "SwapOnAll failed: %v", err)

	// Confirm swap is active
	out, err = exec.Command("swapon", "--show=NAME").CombinedOutput()
	require.NoError(t, err, "swapon --show failed: %s", out)
	require.Contains(t, string(out), swapFile, "swap file not active")

	// Cleanup
	_ = exec.Command("swapoff", swapFile).Run()
}

func TestSwapOffAll_Integration(t *testing.T) {
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

	out, err = exec.Command("mkswap", swapFile).CombinedOutput()
	require.NoError(t, err, "mkswap failed: %s", out)

	out, err = exec.Command("swapon", swapFile).CombinedOutput()
	require.NoError(t, err, "swapon failed: %s", out)

	// Write a temporary fstab with the swap file entry
	tmpFstab, err := os.CreateTemp("", "fstab")
	require.NoError(t, err)
	defer os.Remove(tmpFstab.Name())
	_, err = tmpFstab.WriteString(swapFile + " none swap sw 0 0\n")
	require.NoError(t, err)
	tmpFstab.Close()

	// Patch fstabFile for the test
	fstabFileOrig := fstabFile
	defer func() { fstabFile = fstabFileOrig }()
	fstabFile = tmpFstab.Name()

	// Run SwapOffAll
	code, err := SwapOffAll()
	require.Equal(t, SWAPOFF_EX_OK, code, "SwapOffAll failed: %v", err)

	// Confirm swap is off
	out, err = exec.Command("swapon", "--show=NAME").CombinedOutput()
	require.NoError(t, err, "swapon --show failed: %s", out)
	require.NotContains(t, string(out), swapFile, "swap file still active")

	// Cleanup
	_ = exec.Command("swapoff", swapFile).Run()
}
