//go:build linux

package os

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/joomcode/errorx"
	"golang.org/x/sys/unix"
)

// SwapOff exit code: https://github.com/util-linux/util-linux/blob/master/sys-utils/swapoff.c#L43-L49
// SwapOn Flags: https://github.com/util-linux/util-linux/blob/57d59a5cd5ba6c0b32cae27f5ce48241274f6e6e/sys-utils/swapon.c#L50-L63
const (
	SWAP_EX_ENOMEM  = 2  // swapoff failed due to OOM
	SWAP_EX_FAILURE = 4  // swapoff failed due to other reason
	SWAP_EX_USAGE   = 16 // usage/permissions/syntax error

	SWAPON_FLAG_DISCARD       = 0x10000 // enable discard for swap
	SWAPON_FLAG_DISCARD_ONCE  = 0x20000 // discard swap area at swapon-time
	SWAPON_FLAG_DISCARD_PAGES = 0x40000 // discard page-clusters after use

	ETC_LOCATION           = "/etc"
	FSTAB_LOCATION         = "/etc/fstab"
	PROC_SWAPS_LOCATION    = "/proc/swaps"
	DISK_BY_UUID_LOCATION  = "/dev/disk/by-uuid"
	DISK_BY_LABEL_LOCATION = "/dev/disk/by-label"

	SwapCommentPrefix  = "#" // prefix used when commenting out swap lines in fstab
	swapEntryMinFields = 4
	swapEntryTypeIndex = 2
	swapEntryType      = "swap"
)

var (
	// these are put in variables for easier testing/mocking
	fstabFile      = FSTAB_LOCATION
	swapsFile      = PROC_SWAPS_LOCATION
	uuidLookupDir  = DISK_BY_UUID_LOCATION
	labelLookupDir = DISK_BY_LABEL_LOCATION
)

// swapOn calls the swapon syscall on the given path with the given flags.
// The return error is the errno from the syscall, or nil on success.
// This is set as a variable for easier testing/mocking.
// It assumes path is resolved/normalized (no UUID= or LABEL=)
func sysSwapOn(path string, flags uintptr) error {
	bp, err := unix.BytePtrFromString(path)
	if err != nil {
		return err
	}

	_, _, e := syscall.Syscall(unix.SYS_SWAPON, uintptr(unsafe.Pointer(bp)), flags, 0)
	if e != 0 {
		return e
	}
	return nil
}

// sysSwapOff calls the swapoff syscall on the given path.
// The return error is the errno from the syscall, or nil on success.
// This is set as a variable for easier testing/mocking.
// It assumes path is resolved/normalized (no UUID= or LABEL=)
func sysSwapOff(path string) error {
	bp, err := unix.BytePtrFromString(path)
	if err != nil {
		return err
	}

	_, _, en := syscall.Syscall(unix.SYS_SWAPOFF, uintptr(unsafe.Pointer(bp)), 0, 0)
	if en != 0 {
		return en
	}
	return nil
}

// resolveSpec resolves fstab specs like UUID=xxxx, LABEL=xxxx, or normal paths
// to actual device paths. It returns the resolved path or an error if not found.
// It uses /dev/disk/by-uuid and /dev/disk/by-label for lookups.
// If the spec is already a path, it verifies it exists and returns the real path.
// On error, it returns an empty string and a wrapped errorx error with details.
func resolveSpec(spec string) (string, error) {
	if spec == "" {
		return "", errorx.IllegalArgument.New("spec cannot be empty")
	}

	if strings.HasPrefix(spec, "UUID=") {
		uuid := strings.TrimPrefix(spec, "UUID=")
		path := filepath.Join(uuidLookupDir, uuid)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}

		return "", ErrSwapDeviceNotFound.New("device with UUID %s not found in %s", uuid, uuidLookupDir)
	}

	if strings.HasPrefix(spec, "LABEL=") {
		label := strings.TrimPrefix(spec, "LABEL=")
		path := filepath.Join(labelLookupDir, label)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}

		return "", ErrSwapDeviceNotFound.New("device with LABEL %s not found in %s", label, labelLookupDir)
	}

	// Already a path
	if realPath, err := filepath.EvalSymlinks(spec); err == nil {
		return realPath, nil
	}

	return "", ErrSwapDeviceNotFound.New("failed to resolve device %s", spec)
}

// parseProcSwaps parses /proc/swaps and returns slice of normalized swap source strings.
func parseProcSwaps() ([]string, error) {
	f, err := os.Open(swapsFile)
	if err != nil {
		return nil, ErrFileInaccessible.Wrap(err, "failed to open %s", swapsFile)
	}
	defer f.Close()

	var swaps []string
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return swaps, nil // empty file or missing header line; first line (header) is always skipped if present
	}
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}
		dev := fields[0]
		// Normalize symlinks
		if real, err := filepath.EvalSymlinks(dev); err == nil {
			dev = real
		}
		swaps = append(swaps, dev)
	}

	if err = scanner.Err(); err != nil {
		return nil, ErrFileRead.Wrap(err, "failed to scan %s", swapsFile)
	}
	return swaps, nil
}

// parseFstabSwaps parses /etc/fstab and returns slice of fs_spec strings for entries with vfstype == "swap".
func parseFstabSwaps() ([]string, error) {
	f, err := os.Open(fstabFile)
	if err != nil {
		// some systems may not have /etc/fstab; treat as empty
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, ErrFileInaccessible.Wrap(err, "failed to open %s", fstabFile)
	}
	defer f.Close()

	var swaps []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		// remove any comment portion
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}

		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		fsSpec := fields[0]
		fsVfstype := fields[2]
		if fsVfstype == "swap" {
			resolved, err := resolveSpec(fsSpec)
			if err != nil {
				return nil, err
			}

			swaps = append(swaps, resolved)
		}
	}

	if err = scanner.Err(); err != nil {
		return nil, ErrFileRead.Wrap(err, "failed to scan %s", fstabFile)
	}

	return swaps, nil
}

// check if a given spec is active swap by looking at /proc/swaps listing
// it assumes both spec and activeSwaps are resolved/normalized paths (no UUID= or LABEL=)
func isActiveSwap(spec string, activeSwaps []string) bool {
	for _, s := range activeSwaps {
		if s == spec {
			return true
		}
	}

	return false
}

// isSwapEntry returns true if the line is a valid swap entry in fstab.
func isSwapEntry(line string) bool {
	fields := strings.Fields(line)
	return len(fields) >= swapEntryMinFields && fields[swapEntryTypeIndex] == swapEntryType
}

func handleSyscallErr(err error, path string, operationName string) error {
	if err == nil {
		return nil
	}

	var resultErr error // error to be returned
	if errCode, ok := err.(syscall.Errno); ok {
		switch {
		case errors.Is(errCode, syscall.EPERM):
			resultErr = ErrSwapNotSuperUser.Wrap(err, "%s: %s failed, not super user", path, operationName).
				WithProperty(PathProperty, path).
				WithProperty(SysErrorCodeProperty, SWAP_EX_USAGE)
			return resultErr
		case errors.Is(errCode, syscall.ENOMEM):
			resultErr = ErrSwapOutOfMemory.Wrap(err, "%s: %s failed, cannot allocate memory", path, operationName).
				WithProperty(PathProperty, path).
				WithProperty(SysErrorCodeProperty, SWAP_EX_ENOMEM)
			return resultErr
		default:
			resultErr = ErrSwapUnknownSyscall.Wrap(err, "%s: %s failed, unknown syscall error", path, operationName).
				WithProperty(PathProperty, path).
				WithProperty(SysErrorCodeProperty, SWAP_EX_FAILURE)
			return resultErr
		}
	}

	resultErr = ErrNonSyscall.Wrap(err, "%s: %s failed, non syscall error", path, operationName).
		WithProperty(PathProperty, path).
		WithProperty(SysErrorCodeProperty, SWAP_EX_FAILURE)

	return resultErr
}

// SwapOff performs swapoff for a single path
// On success, it returns SWAP_EX_OK and nil error.
// On error, it returns a non-zero status code and a wrapped errorx error with details.
func SwapOff(path string) error {
	err := sysSwapOff(path)
	return handleSyscallErr(err, path, "swapoff")
}

// SwapOffAll attempts to disable all swap devices/files on the system based on /proc/swaps and /etc/fstab.
func SwapOffAll() error {
	// 1) swapOff everything listed in /proc/swaps (quiet, but track status)
	activeSwaps, err := parseProcSwaps()
	if err != nil {
		return err
	}

	for _, src := range activeSwaps {
		err = SwapOff(src)
		if err != nil {
			return err // fail first
		}
	}

	// 2) also check /etc/fstab for swap entries and call swapoff on them if not active.
	fstabSwaps, err := parseFstabSwaps()
	if err != nil {
		return err
	}

	for _, spec := range fstabSwaps {
		// If spec is currently active swap, we already attempted swapOff earlier, so a nop.
		if isActiveSwap(spec, activeSwaps) {
			continue
		}

		err = SwapOff(spec)
		if err != nil {
			return err
		}
	}

	return nil
}

func SwapOn(path string, flags int) error {
	err := sysSwapOn(path, uintptr(flags))
	return handleSyscallErr(err, path, "swapon")
}

func SwapOnAll() error {
	fstabSwaps, err := parseFstabSwaps()
	if err != nil {
		return err
	}

	if len(fstabSwaps) == 0 {
		return nil
	}

	for _, spec := range fstabSwaps {
		err = SwapOn(spec, 0)
		if err != nil {
			return err // fail first
		}
	}

	return nil
}

func updateFstabFile(modifier func(line string) string) error {
	if modifier == nil {
		return errorx.IllegalArgument.New("line modifier function cannot be nil")
	}

	// Read original file
	input, err := os.ReadFile(fstabFile)
	if err != nil {
		return ErrFileInaccessible.Wrap(err, "failed to read fstab file: %s", fstabFile)
	}

	info, err := os.Stat(fstabFile)
	if err != nil {
		return ErrFileInaccessible.Wrap(err, "failed to stat fstab file: %s", fstabFile)
	}

	// Process lines
	var output []string
	scanner := bufio.NewScanner(strings.NewReader(string(input)))
	for scanner.Scan() {
		line := scanner.Text()
		// Only comment if the line is a valid swap entry (at least 4 fields and type is "swap")
		if isSwapEntry(line) {
			line = modifier(line)
		}
		output = append(output, line)
	}

	if err = scanner.Err(); err != nil {
		return ErrFileRead.Wrap(err, "failed to scan fstab file: %s", fstabFile)
	}

	// Write modified file
	err = os.WriteFile(fstabFile, []byte(strings.Join(output, "\n")+"\n"), info.Mode())
	if err != nil {
		return ErrFileWrite.Wrap(err, "failed to write fstab file: %s", fstabFile)
	}

	return nil
}

func commentOutSwapLine(line string) string {
	if isSwapEntry(line) && !strings.HasPrefix(line, SwapCommentPrefix) {
		return SwapCommentPrefix + line
	}
	return line
}

func uncommentSwapLine(line string) string {
	if strings.HasPrefix(line, SwapCommentPrefix) {
		uncommented := strings.TrimLeft(line, SwapCommentPrefix)
		if isSwapEntry(uncommented) {
			return uncommented
		}
	}
	return line
}

// DisableSwap comments out any swap entries in /etc/fstab to disable swap on reboot
// It finds lines in /etc/fstab containing the word "swap" and comments them out by prefixing with #
// It then calls SwapOffAll to immediately disable any active swap
// On error, it returns a wrapped errorx error with details.
func DisableSwap() error {
	if err := SwapOffAll(); err != nil {
		return err
	}

	return updateFstabFile(commentOutSwapLine)
}

// EnableSwap uncomments any swap entries in /etc/fstab to enable swap on reboot
// It finds lines in /etc/fstab that are commented out and contain the word "swap" and uncomments them by removing the leading #
// It then calls SwapOnAll to immediately enable any swap entries found in /etc/fstab
// On error, it returns a wrapped errorx error with details.
func EnableSwap() error {
	err := updateFstabFile(uncommentSwapLine)

	if err != nil {
		return err
	}

	return SwapOnAll()
}
