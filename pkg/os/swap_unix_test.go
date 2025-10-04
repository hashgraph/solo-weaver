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

	content := "Filename\tType\tSize\tUsed\tPriority\n/dev/vda1 partition 12345 0 -2\n/swapfile file 12345 0 -3\n"
	if _, err := tmp.WriteString(content); err != nil {
		t.Fatal(err)
	}
	require.NoError(t, tmp.Close())

	swaps, err := parseProcSwaps()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(swaps) != 2 || swaps[0] != "/dev/vda1" || swaps[1] != "/swapfile" {
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
/dev/vda1   none   swap   sw   0   0
/dev/vda2   /mnt   ext4   defaults   0   0
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
	if len(swaps) != 2 || swaps[0] != "/dev/vda1" || swaps[1] != swapFile {
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
/dev/vda1 none swap sw 0 0
`
	if _, err := tmp.WriteString(content); err != nil {
		t.Fatal(err)
	}
	require.NoError(t, tmp.Close())

	swaps, err := parseFstabSwaps()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(swaps) != 1 || swaps[0] != "/dev/vda1" {
		t.Errorf("unexpected swaps: %v", swaps)
	}
}

func TestIsActiveSwap(t *testing.T) {
	tests := []struct {
		spec        string
		activeSwaps []string
		want        bool
	}{
		{"/dev/vda1", []string{"/dev/vda1", "/swapfile"}, true},
		{"/swapfile", []string{"/dev/vda1", "/swapfile"}, true},
		{"/dev/vdb1", []string{"/dev/vda1", "/swapfile"}, false},
		{"", []string{}, false},
		{"/dev/vda1", []string{}, false},
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
	unrelatedLine := "/dev/vda1 / ext4 defaults 0 1"

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
				"#/dev/vda1 / ext4 defaults 0 1",
				"UUID=123 none swap sw 0 0",
			},
			comment: true,
			expected: []string{
				"#/dev/vda1 / ext4 defaults 0 1",
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
