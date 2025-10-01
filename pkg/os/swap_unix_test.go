//go:build linux

package os

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
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

func TestParseProcSwaps_Normal(t *testing.T) {
	tmp, err := os.CreateTemp("", "swaps")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := os.Remove(tmp.Name())
		if err != nil {
			t.Errorf("failed to remove temp file: %v", err)
		}
	}()
	swapsFileOrig := swapsFile
	defer func() { swapsFile = swapsFileOrig }()
	swapsFile = tmp.Name()

	content := "Filename\tType\tSize\tUsed\tPriority\n/dev/sda1 partition 12345 0 -2\n/swapfile file 12345 0 -3\n"
	if _, err := tmp.WriteString(content); err != nil {
		t.Fatal(err)
	}
	require.NoError(t, tmp.Close())

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
	defer func() {
		err := os.Remove(tmp.Name())
		if err != nil {
			t.Errorf("failed to remove temp file: %v", err)
		}
	}()
	swapsFileOrig := swapsFile
	defer func() { swapsFile = swapsFileOrig }()
	swapsFile = tmp.Name()

	require.NoError(t, tmp.Close())
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
	f := makeTestSwapFile(t)
	defer func() {
		_ = os.Remove(f.Name())
	}()
	swapFile := f.Name()

	tmp, err := os.CreateTemp("", "fstab")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := os.Remove(tmp.Name())
		if err != nil {
			t.Errorf("failed to remove temp file: %v", err)
		}
	}()
	fstabFileOrig := fstabFile
	defer func() { fstabFile = fstabFileOrig }()
	fstabFile = tmp.Name()

	content := fmt.Sprintf(`
# comment line
/dev/sda1   none   swap   sw   0   0
/dev/sda2   /mnt   ext4   defaults   0   0
%s    none   swap   sw   0   0
`, swapFile)
	if _, err := tmp.WriteString(content); err != nil {
		t.Fatal(err)
	}
	require.NoError(t, tmp.Close())

	swaps, err := parseFstabSwaps()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(swaps) != 2 || swaps[0] != "/dev/sda1" || swaps[1] != swapFile {
		t.Errorf("unexpected swaps: %v", swaps)
	}
}

func TestParseFstabSwaps_EmptyFile(t *testing.T) {
	tmp, err := os.CreateTemp("", "fstab")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := os.Remove(tmp.Name())
		if err != nil {
			t.Errorf("failed to remove temp file: %v", err)
		}
	}()
	fstabFileOrig := fstabFile
	defer func() { fstabFile = fstabFileOrig }()
	fstabFile = tmp.Name()

	require.NoError(t, tmp.Close())
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
	defer func() {
		err := os.Remove(tmp.Name())
		if err != nil {
			t.Errorf("failed to remove temp file: %v", err)
		}
	}()
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
	require.NoError(t, tmp.Close())

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

func TestHandleSyscallErr(t *testing.T) {
	tests := []struct {
		name         string
		inputErr     error
		wantMsg      string
		wantType     *errorx.Type
		wantCodeProp int
	}{
		{
			name:         "EPERM",
			inputErr:     syscall.EPERM,
			wantMsg:      "not super user",
			wantType:     ErrSwapNotSuperUser,
			wantCodeProp: SWAP_EX_USAGE,
		},
		{
			name:         "ENOMEM",
			inputErr:     syscall.ENOMEM,
			wantMsg:      "cannot allocate memory",
			wantType:     ErrSwapOutOfMemory,
			wantCodeProp: SWAP_EX_ENOMEM,
		},
		{
			name:         "UnknownSyscall",
			inputErr:     syscall.Errno(123),
			wantMsg:      "unknown syscall error",
			wantType:     ErrSwapUnknownSyscall,
			wantCodeProp: SWAP_EX_FAILURE,
		},
		{
			name:         "NonSyscall",
			inputErr:     fmt.Errorf("other error"),
			wantMsg:      "non syscall error",
			wantType:     ErrNonSyscall,
			wantCodeProp: SWAP_EX_FAILURE,
		},
		{
			name:     "NilError",
			inputErr: nil,
			wantMsg:  "",
			wantType: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := "/dev/swap"
			err := handleSyscallErr(tt.inputErr, path, "swapon")
			if tt.inputErr == nil {
				require.Nil(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantMsg)
			if tt.wantType != nil {
				require.True(t, errorx.IsOfType(err, tt.wantType))
			}
			// Check PathProperty
			if tt.wantType != nil {
				pathProp, ok := errorx.ExtractProperty(err, PathProperty)
				require.True(t, ok)
				require.Equal(t, path, pathProp)
				codeProp, ok := errorx.ExtractProperty(err, SysErrorCodeProperty)
				require.True(t, ok)
				require.Equal(t, tt.wantCodeProp, codeProp)
			}
		})
	}
}

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
	if string(out) != "NAME\n" && !os.IsNotExist(err) {
		if string(out) != "" && !os.IsNotExist(err) && string(out) != "NAME\n" {
			t.Errorf("swap file still active: %s", out)
		}
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

func makeTestSwapFile(t *testing.T) *os.File {
	h, err := os.UserHomeDir()
	require.NoError(t, err)

	// swapfile needs to be in ext4 or swapoff fails with "swapoff: /path: Invalid argument"
	f, err := os.CreateTemp(h, "swap-test-")
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

func TestUpdateFstabFile(t *testing.T) {
	// make a temp fstab file for testing
	fstabFileOrig := fstabFile
	defer func() {
		fstabFile = fstabFileOrig
	}()

	// Define test lines
	swapLine := "UUID=123 none swap sw 0 0"
	commentedSwapLine := "#UUID=123 none swap sw 0 0"
	unrelatedComment := "# swap was on /dev/vda4 during installation"
	unrelatedLine := "/dev/sda1 / ext4 defaults 0 1"

	cases := []struct {
		name     string
		input    []string
		comment  bool
		expected []string
	}{
		{
			name: "Uncomment swap line",
			input: []string{
				commentedSwapLine,
				unrelatedComment,
				unrelatedLine,
			},
			comment: false,
			expected: []string{
				swapLine,
				unrelatedComment,
				unrelatedLine,
			},
		},
		{
			name: "Comment swap line",
			input: []string{
				swapLine,
				unrelatedComment,
				unrelatedLine,
			},
			comment: true,
			expected: []string{
				commentedSwapLine,
				unrelatedComment,
				unrelatedLine,
			},
		},
		{
			name: "Ignore unrelated comment with swap word",
			input: []string{
				unrelatedComment,
				swapLine,
			},
			comment: true,
			expected: []string{
				unrelatedComment,
				commentedSwapLine,
			},
		},
		{
			name: "Commented non-swap line remains unchanged",
			input: []string{
				"#/dev/sda1 / ext4 defaults 0 1",
				"UUID=123 none swap sw 0 0",
			},
			comment: true,
			expected: []string{
				"#/dev/sda1 / ext4 defaults 0 1",
				"#UUID=123 none swap sw 0 0",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp, err := os.CreateTemp("", "fstab-test")
			require.NoError(t, err)
			defer os.Remove(tmp.Name())

			// Write input lines to temp file
			require.NoError(t, os.WriteFile(tmp.Name(), []byte(strings.Join(tc.input, "\n")+"\n"), 0600))

			fstabFile = tmp.Name()
			if tc.comment {
				err = updateFstabFile(commentOutSwapLine)
			} else {
				err = updateFstabFile(uncommentSwapLine)
			}

			require.NoError(t, err)

			// Read back and compare
			content, err := os.ReadFile(fstabFile)
			require.NoError(t, err)
			lines := strings.Split(strings.TrimRight(string(content), "\n"), "\n")
			require.Equal(t, tc.expected, lines)
		})
	}
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
