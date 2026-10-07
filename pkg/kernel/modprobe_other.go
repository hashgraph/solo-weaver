// SPDX-License-Identifier: Apache-2.0

//go:build !linux

package kernel

import "github.com/joomcode/errorx"

// Loading and unloading kernel modules relies on Linux-only modprobe syscalls
// (see modprobe_linux.go). On other platforms (e.g. macOS used for development)
// these return an unsupported-operation error so the package compiles and its
// platform-agnostic unit tests still build and run.

func modprobeLoad(_ string) error {
	return errorx.UnsupportedOperation.New("loading kernel modules is only supported on Linux")
}

func modprobeRemove(_ string) error {
	return errorx.UnsupportedOperation.New("unloading kernel modules is only supported on Linux")
}
