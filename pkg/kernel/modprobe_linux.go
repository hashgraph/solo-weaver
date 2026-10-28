// SPDX-License-Identifier: Apache-2.0

//go:build linux

package kernel

import "pault.ag/go/modprobe"

// modprobeLoad loads a kernel module via the Linux modprobe syscalls.
// The name is validated by the caller before it reaches this function.
func modprobeLoad(name string) error {
	return modprobe.Load(name, "")
}

// modprobeRemove unloads a kernel module via the Linux modprobe syscalls.
// The name is validated by the caller before it reaches this function.
func modprobeRemove(name string) error {
	return modprobe.Remove(name)
}
