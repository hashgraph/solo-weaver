// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package ui

import (
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// CaptureOutput redirects file descriptors 1 (stdout) and 2 (stderr) to
// discard pipes at the OS level using dup2. This catches ALL output — even
// from third-party C/Go libraries that captured the fd at init time or write
// directly to fd 1/2 (e.g. Helm OCI "Pulled:", syspkg "apt manager").
//
// Returns an *os.File backed by the original stdout fd (for the output handler
// to write to) and a cleanup function that restores both fds.
func CaptureOutput() (origStdout *os.File, cleanup func()) {
	// Duplicate the original fds so we can write to the real terminal later.
	origStdoutFd, err := unix.Dup(1)
	if err != nil {
		return os.Stdout, func() {}
	}
	origStderrFd, err := unix.Dup(2)
	if err != nil {
		unix.Close(origStdoutFd)
		return os.Stdout, func() {}
	}

	origStdout = os.NewFile(uintptr(origStdoutFd), "/dev/stdout")

	// Create a pipe whose read-end is drained (discarded).
	r, w, err := os.Pipe()
	if err != nil {
		unix.Close(origStdoutFd)
		unix.Close(origStderrFd)
		return os.Stdout, func() {}
	}
	go func() { _, _ = io.Copy(io.Discard, r) }()

	// Redirect fd 1 and fd 2 to the pipe's write-end.
	// This is atomic at the kernel level and affects all goroutines/libraries.
	_ = unix.Dup2(int(w.Fd()), 1)
	_ = unix.Dup2(int(w.Fd()), 2)

	// Update the Go-level variables so fmt.Print / log.Print also go to the pipe.
	os.Stdout = os.NewFile(1, "/dev/stdout")
	os.Stderr = os.NewFile(2, "/dev/stderr")

	return origStdout, func() {
		// Restore the original fds.
		_ = unix.Dup2(origStdoutFd, 1)
		_ = unix.Dup2(origStderrFd, 2)
		os.Stdout = os.NewFile(1, "/dev/stdout")
		os.Stderr = os.NewFile(2, "/dev/stderr")

		w.Close()
		r.Close()
		origStdout.Close()
		unix.Close(origStdoutFd)
		unix.Close(origStderrFd)
	}
}
