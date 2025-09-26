package os

import (
	"bufio"
	"errors"
	"github.com/joomcode/errorx"
	"golang.org/x/sys/unix"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// Return codes similar to the C sample
// Ref:
// - SwapOff exit code: https://github.com/util-linux/util-linux/blob/master/sys-utils/swapoff.c#L43-L49
// - SwapOn Flags: https://github.com/util-linux/util-linux/blob/57d59a5cd5ba6c0b32cae27f5ce48241274f6e6e/sys-utils/swapon.c#L50-L63
const (
	SWAPOFF_EX_OK      = 0  // no errors
	SWAPOFF_EX_ENOMEM  = 2  // swapoff failed due to OOM
	SWAPOFF_EX_FAILURE = 4  // swapoff failed due to other reason
	SWAPOFF_EX_SYSERR  = 8  // non-swapoff errors
	SWAPOFF_EX_USAGE   = 16 // usage/permissions/syntax error
	SWAPOFF_EX_ALLERR  = 32 // --all all failed
	SWAPOFF_EX_SOMEOK  = 64 // --all some failed some OK

	SWAPON_FLAG_DISCARD       = 0x10000 // enable discard for swap
	SWAPON_FLAG_DISCARD_ONCE  = 0x20000 // discard swap area at swapon-time
	SWAPON_FLAG_DISCARD_PAGES = 0x40000 // discard page-clusters after use
)

var (
	// these are put in variables for easier testing/mocking
	fstabFile = "/etc/fstab"
	swapsFile = "/proc/swaps"

	PathProperty       = errorx.RegisterProperty("path")
	ReturnCodeProperty = errorx.RegisterProperty("return_code")

	// swapOn calls the swapon syscall on the given path with the given flags.
	// The return error is the errno from the syscall, or nil on success.
	// This is set as a variable for easier testing/mocking.
	sysSwapOn = func(path string, flags uintptr) error {
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
	sysSwapOff = func(path string) error {
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
)

// parseProcSwaps parses /proc/swaps and returns slice of swap source strings (first column).
func parseProcSwaps() ([]string, error) {
	f, err := os.Open(swapsFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var swaps []string
	scanner := bufio.NewScanner(f)
	// skip header line
	if !scanner.Scan() {
		// empty file/no header
		return swaps, nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}
		// first field is the swap filename/device
		swaps = append(swaps, fields[0])
	}
	if err := scanner.Err(); err != nil {
		return nil, err
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
		return nil, err
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
			swaps = append(swaps, fsSpec)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return swaps, nil
}

// check if a given spec is active swap by looking at /proc/swaps listing
func isActiveSwap(spec string, activeSwaps []string) bool {
	for _, s := range activeSwaps {
		if s == spec {
			return true
		}
	}
	return false
}

// SwapOff performs swapoff for a single path
// On success, it returns SWAPOFF_EX_OK and nil error.
// On error, it returns a non-zero status code and a wrapped errorx error with details.
func SwapOff(path string) (int, error) {
	err := sysSwapOff(path)
	if err == nil {
		return SWAPOFF_EX_OK, nil
	}

	// map errno to return codes
	var rex error // error to be returned
	if errno, ok := err.(syscall.Errno); ok {
		switch errno {
		case syscall.EPERM:
			rex = errorx.IllegalState.Wrap(err, "%s: swapoff failed, not super user", path).
				WithProperty(PathProperty, path).
				WithProperty(ReturnCodeProperty, SWAPOFF_EX_USAGE)
			return SWAPOFF_EX_USAGE, rex
		case syscall.ENOMEM:
			rex = errorx.IllegalState.Wrap(err, "%s: swapoff failed, cannot allocate memory", path).
				WithProperty(PathProperty, path).
				WithProperty(ReturnCodeProperty, SWAPOFF_EX_ENOMEM)
			return SWAPOFF_EX_ENOMEM, err
		default:
			rex = errorx.IllegalState.Wrap(err, "%s: swapoff failed, unknown syscall error", path).
				WithProperty(PathProperty, path).
				WithProperty(ReturnCodeProperty, SWAPOFF_EX_FAILURE)
			return SWAPOFF_EX_FAILURE, rex
		}
	}

	rex = errorx.IllegalState.Wrap(err, "%s: swapoff failed, non syscall error", path).
		WithProperty(PathProperty, path).
		WithProperty(ReturnCodeProperty, SWAPOFF_EX_FAILURE)
	return SWAPOFF_EX_FAILURE, rex
}

// SwapOffAll attempts to disable all swap devices/files on the system based on /proc/swaps and /etc/fstab.
//
// The return status codes are similar to the C code.
// See SWAPOFF_EX_* constants for details.
//
// On success, returns integer status code and nil error.
// On error, returns non-zero status code and a wrapped errorx error with details.
func SwapOffAll() (int, error) {
	var swapOfErrors []error
	var totalSuccess int

	// 1) swapOff everything listed in /proc/swaps (quiet, but track status)
	activeSwaps, err := parseProcSwaps()
	if err != nil {
		return SWAPOFF_EX_SYSERR, errorx.IllegalFormat.Wrap(err, "failed to read /proc/swaps")
	}

	for _, src := range activeSwaps {
		rc, err := SwapOff(src)
		if rc == SWAPOFF_EX_OK {
			totalSuccess++
		} else {
			swapOfErrors = append(swapOfErrors, err)
		}
	}

	// 2) also check /etc/fstab for swap entries and call swapoff on them if not active.
	fstabSwaps, err := parseFstabSwaps()
	if err != nil {
		// treat parse error as system error
		return SWAPOFF_EX_SYSERR, errorx.IllegalFormat.
			Wrap(err, "failed to parse %s", fstabFile).
			WithProperty(PathProperty, fstabFile)
	}
	for _, spec := range fstabSwaps {
		// If spec is currently active swap, we already attempted swapOff earlier, so a nop.
		if isActiveSwap(spec, activeSwaps) {
			continue
		}

		rc, err := SwapOff(spec)
		if rc == SWAPOFF_EX_OK {
			totalSuccess++
		} else {
			swapOfErrors = append(swapOfErrors, err)
		}
	}

	var rex error
	if len(swapOfErrors) > 0 {
		rex = errorx.WrapMany(errorx.IllegalState, "some swapoff operations failed", swapOfErrors...)
	}

	// Decide return code
	if len(swapOfErrors) == 0 {
		return SWAPOFF_EX_OK, nil
	} else if totalSuccess == 0 {
		return SWAPOFF_EX_ALLERR, rex
	}

	return SWAPOFF_EX_SOMEOK, rex
}

func SwapOn(path string, flags int) (int, error) {
	err := sysSwapOn(path, uintptr(flags))
	if err != nil {
		var rex error
		// map some common errno for informative messages
		if errno, ok := err.(syscall.Errno); ok {
			switch errno {
			case syscall.EPERM:
				rex = errorx.IllegalState.Wrap(err, "%s: swapon failed, not super user", path).
					WithProperty(PathProperty, path).
					WithProperty(ReturnCodeProperty, SWAPOFF_EX_USAGE)
				return SWAPOFF_EX_USAGE, rex
			default:
				rex = errorx.IllegalState.Wrap(err, "%s: swapon failed, unknown syscall error", path).
					WithProperty(PathProperty, path).
					WithProperty(ReturnCodeProperty, SWAPOFF_EX_FAILURE)
				return SWAPOFF_EX_FAILURE, rex
			}
		}

		rex = errorx.IllegalState.Wrap(err, "%s: swapon failed, non syscall error", path).
			WithProperty(PathProperty, path).
			WithProperty(ReturnCodeProperty, SWAPOFF_EX_FAILURE)
		return SWAPOFF_EX_FAILURE, rex
	}

	return SWAPOFF_EX_OK, nil
}

func SwapOnAll() (int, error) {
	fstabSwaps, err := parseFstabSwaps()
	if err != nil {
		return SWAPOFF_EX_SYSERR, errorx.IllegalFormat.
			Wrap(err, "failed to parse %s", fstabFile).
			WithProperty(PathProperty, fstabFile)
	}

	if len(fstabSwaps) == 0 {
		return SWAPOFF_EX_OK, nil
	}

	nerrs := 0
	nsucc := 0
	var swapOnErrors []error

	for _, spec := range fstabSwaps {
		_, err := SwapOn(spec, 0)
		if err != nil {
			var errno syscall.Errno
			if errors.As(err, &errno) && errors.Is(errno, syscall.EPERM) {
				swapOnErrors = append(swapOnErrors, errorx.IllegalState.Wrap(err, "%s: Not superuser.", spec))
				return SWAPOFF_EX_USAGE, errorx.WrapMany(errorx.IllegalState, "Not superuser for some swaps", swapOnErrors...)
			}
			swapOnErrors = append(swapOnErrors, errorx.IllegalState.Wrap(err, "%s: swapon failed", spec))
			nerrs++
		} else {
			nsucc++
		}
	}

	if nerrs == 0 {
		return SWAPOFF_EX_OK, nil
	} else if nsucc == 0 {
		return SWAPOFF_EX_ALLERR, errorx.WrapMany(errorx.IllegalState, "All swapon operations failed", swapOnErrors...)
	}
	return SWAPOFF_EX_SOMEOK, errorx.WrapMany(errorx.IllegalState, "Some swapon operations failed", swapOnErrors...)
}
