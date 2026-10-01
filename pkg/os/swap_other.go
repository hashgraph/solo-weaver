// SPDX-License-Identifier: Apache-2.0

//go:build !linux

// Swap management relies on Linux-only swapon/swapoff syscalls, /proc/swaps,
// and systemd (see swap_unix.go). This file provides stubs for all other
// platforms (e.g. macOS used for development) so the package — and everything
// that imports it — still compiles and its platform-agnostic unit tests build.
// Every exported function returns an unsupported-operation error; these code
// paths are never exercised outside a Linux target host.

package os

import "github.com/joomcode/errorx"

// SwapOn flags, kept in parity with the Linux implementation.
const (
	SWAPON_FLAG_DISCARD       = 0x10000
	SWAPON_FLAG_DISCARD_ONCE  = 0x20000
	SWAPON_FLAG_DISCARD_PAGES = 0x40000
)

var errSwapUnsupported = errorx.UnsupportedOperation.New("swap operations are only supported on Linux")

// SwapOff is unsupported on non-Linux platforms.
func SwapOff(_ string) error { return errSwapUnsupported }

// SwapOffAll is unsupported on non-Linux platforms.
func SwapOffAll() error { return errSwapUnsupported }

// SwapOn is unsupported on non-Linux platforms.
func SwapOn(_ string, _ int) error { return errSwapUnsupported }

// SwapOnAll is unsupported on non-Linux platforms.
func SwapOnAll() error { return errSwapUnsupported }

// DisableSwap is unsupported on non-Linux platforms.
func DisableSwap() error { return errSwapUnsupported }

// EnableSwap is unsupported on non-Linux platforms.
func EnableSwap() error { return errSwapUnsupported }
