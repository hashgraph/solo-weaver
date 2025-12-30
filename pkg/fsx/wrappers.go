// SPDX-License-Identifier: Apache-2.0

package fsx

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"github.com/joomcode/errorx"
)

// Close closes the file and logs an error if it fails.
// It's a wrapper of file.Close without the need for the caller to handle the error.
func Close(f *os.File) {
	if f == nil {
		return
	}

	err := f.Close()
	if err != nil {
		// Prefer errors.Is over string matching. Treat already-closed / bad-fd as non-errors.
		if errors.Is(err, os.ErrClosed) || errors.Is(err, syscall.EBADF) {
			return
		}

		fmt.Printf("ERROR: %+v\n", errorx.Decorate(err, "failed to close file %q", f.Name()))
	}
}

// Remove removes the file at the given path and logs an error if it fails.
// It's a wrapper of os.Remove without the need for the caller to handle the error.
func Remove(path string) {
	if path == "" {
		return
	}

	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		fmt.Printf("ERROR: %+v\n", errorx.Decorate(err, "failed to remove file %q", path))
	}
}

// RemoveAll removes the path and its contents and logs an error if it fails.
// It's a wrapper of os.RemoveAll without the need for the caller to handle the error.
func RemoveAll(path string) {
	if path == "" {
		return
	}

	err := os.RemoveAll(path)
	if err != nil && !os.IsNotExist(err) {
		fmt.Printf("ERROR: %+v\n", errorx.Decorate(err, "failed to remove all at path %q", path))
	}
}
