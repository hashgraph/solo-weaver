// SPDX-License-Identifier: Apache-2.0

//go:build windows

package ui

import "os"

// CaptureOutput is a no-op on Windows — fd-level redirection is not supported.
func CaptureOutput() (origStdout *os.File, cleanup func()) {
	return os.Stdout, func() {}
}
