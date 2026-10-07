// SPDX-License-Identifier: Apache-2.0

//go:build linux

package mount

import (
	"os"

	"github.com/hashgraph/solo-weaver/internal/mount/mountinfo"
	"github.com/joomcode/errorx"
)

const DefaultProcMountInfoFile = "/proc/self/mountinfo"

// use var to allow mocking in tests
var procMountInfoFile = DefaultProcMountInfoFile

// ClassifyStoragePaths reports the mount backing each volume-name -> path entry.
// It reads /proc/self/mountinfo, not /proc/mounts (used by GetMountsUnderPath),
// because mountinfo lists every mount including bind mounts, which the
// longest-prefix resolution needs.
func ClassifyStoragePaths(paths map[string]string) ([]mountinfo.Finding, error) {
	f, err := os.Open(procMountInfoFile)
	if err != nil {
		return nil, errorx.ExternalError.Wrap(err, "failed to open %s", procMountInfoFile)
	}
	defer func() { _ = f.Close() }()

	entries, err := mountinfo.Parse(f)
	if err != nil {
		return nil, err
	}

	return mountinfo.Classify(paths, entries, mountinfo.NetworkTransport), nil
}
