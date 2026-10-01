// SPDX-License-Identifier: Apache-2.0

//go:build !linux

// ClassifyStoragePaths reads /proc/self/mountinfo, which is Linux-only (see
// mountinfo_linux.go). This stub lets the package — and its callers in
// internal/workflows/steps — compile and run their platform-agnostic unit
// tests on other platforms (e.g. macOS used for development). It returns an
// unsupported-operation error; the real path only runs on a Linux target host.

package mount

import (
	"github.com/hashgraph/solo-weaver/internal/mount/mountinfo"
	"github.com/joomcode/errorx"
)

const DefaultProcMountInfoFile = "/proc/self/mountinfo"

// ClassifyStoragePaths is unsupported on non-Linux platforms.
func ClassifyStoragePaths(_ map[string]string) ([]mountinfo.Finding, error) {
	return nil, errorx.UnsupportedOperation.New("storage-path classification is only supported on Linux")
}
