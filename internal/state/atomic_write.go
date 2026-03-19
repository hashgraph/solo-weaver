// SPDX-License-Identifier: Apache-2.0

// atomic_write.go provides the single canonical implementation of atomic file
// writes for the state package.
//
// Both the state manager (FlushState) and state migrations use atomicWriteFile
// to persist YAML to disk. Keeping a single implementation ensures that
// permissions, fsync behaviour, and error handling stay consistent across all
// write paths and that future fixes only need to be applied in one place.
//
// Write sequence (POSIX atomic):
//  1. MkdirAll — ensure the parent directory exists.
//  2. CreateTemp — open a sibling temp file in the same directory (same filesystem,
//     same mount point) so the final rename is always atomic.
//  3. Write + Sync — flush data and metadata to the storage device before rename.
//  4. Close — release the file descriptor before rename (required on Windows;
//     harmless and correct on POSIX).
//  5. Rename — atomically replace the target path.
//  6. Cleanup on failure — remove the temp file if any step fails.

package state

import (
	"os"
	"path/filepath"
)

// atomicWriteFile writes data to path using a write-to-temp-then-rename strategy,
// ensuring that readers never observe a partially-written file.
//
// The parent directory is created with mode 0o755 if it does not exist.
// The temporary file is created in the same directory as path so the final
// os.Rename is guaranteed to be atomic (same filesystem).
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".state.tmp.*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	success := false
	defer func() {
		_ = tmp.Close() // no-op if already closed below; error intentionally ignored in defer
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	// Close explicitly before rename: required on Windows, recommended on POSIX.
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	success = true
	return nil
}
