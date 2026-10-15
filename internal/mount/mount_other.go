// SPDX-License-Identifier: Apache-2.0

//go:build !linux

// Package mount provides bind-mount and fstab management. The real
// implementation (mount_unix.go) is Linux-only because it relies on Linux
// mount syscalls, /proc/mounts, and /etc/fstab. This file provides stubs for
// all other platforms (e.g. macOS) so the package — and everything that
// imports it — still compiles and its unit tests build off a Linux host. Every
// exported function returns an unsupported-operation error; these code paths
// are never exercised outside a Linux target host.
package mount

import "github.com/joomcode/errorx"

const (
	DefaultFstabFile      = "/etc/fstab"
	DefaultProcMountsFile = "/proc/mounts"
)

// BindMount mirrors the Linux definition so callers compile on all platforms.
type BindMount struct {
	Source string
	Target string
}

var errUnsupported = errorx.UnsupportedOperation.New("mount operations are only supported on Linux")

// SetupBindMountsWithFstab is unsupported on non-Linux platforms.
func SetupBindMountsWithFstab(_ BindMount) error {
	return errUnsupported
}

// RemoveBindMountsWithFstab is unsupported on non-Linux platforms.
func RemoveBindMountsWithFstab(_ BindMount) error {
	return errUnsupported
}

// GetMountsUnderPath is unsupported on non-Linux platforms.
func GetMountsUnderPath(_ string) ([]string, error) {
	return nil, errUnsupported
}

// UnmountPath is unsupported on non-Linux platforms.
func UnmountPath(_ string) error {
	return errUnsupported
}

// IsBindMountedWithFstab is unsupported on non-Linux platforms.
func IsBindMountedWithFstab(_ BindMount) (alreadyMounted bool, fstabEntryAlreadyAdded bool, err error) {
	return false, false, errUnsupported
}
