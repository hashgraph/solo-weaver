// SPDX-License-Identifier: Apache-2.0

package fsx

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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

// AtomicWriteFile writes payload to path using a write-to-temp-then-rename strategy,
// ensuring readers never observe a partially-written file. The temp file is created in
// the same directory as path (guaranteeing a same-filesystem rename). perm is applied
// to the temp file before the rename so the final file is never visible with incorrect
// permissions. On any failure the temp file is removed before returning.
func AtomicWriteFile(path string, payload []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	tmp, err := os.CreateTemp(dir, "."+base+".tmp.*")
	if err != nil {
		return FileWriteError.New("failed to create temp file in %s", dir).WithUnderlyingErrors(err)
	}
	tmpPath := tmp.Name()
	success := false
	defer func() {
		_ = tmp.Close()
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()

	n, err := tmp.Write(payload)
	if err != nil {
		return FileWriteError.New("failed to write temp file %s", tmpPath).WithUnderlyingErrors(err)
	}
	if n != len(payload) {
		return FileWriteError.New("short write to temp file %s: wrote %d of %d bytes", tmpPath, n, len(payload))
	}

	if err := tmp.Sync(); err != nil {
		return FileWriteError.New("failed to sync temp file %s", tmpPath).WithUnderlyingErrors(err)
	}

	if err := tmp.Close(); err != nil {
		return FileWriteError.New("failed to close temp file %s", tmpPath).WithUnderlyingErrors(err)
	}

	if err := os.Chmod(tmpPath, perm); err != nil {
		return FileWriteError.New("failed to set permissions on temp file %s", tmpPath).WithUnderlyingErrors(err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return FileWriteError.New("failed to rename %s to %s", tmpPath, path).WithUnderlyingErrors(err)
	}

	success = true
	return nil
}
