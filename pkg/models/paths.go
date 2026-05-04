// SPDX-License-Identifier: Apache-2.0

package models

import "io/fs"

const (
	// File and directory permissions
	DefaultDirOrExecPerm = 0o755 // for directories and executable files
	// DefaultStorageDirPerm is 2775 with setgid. Must use fs.ModeSetgid (1<<23), not the
	// Unix octal 02000, because Go's os.Chmod checks fs.ModeSetgid — passing 0o2775 directly
	// silently drops the setgid bit (0o2775 & (1<<23) == 0).
	DefaultStorageDirPerm = fs.ModeSetgid | 0o775
	DefaultFilePerm       = 0o644 // for regular data/config files

	// Weaver paths
	DefaultWeaverHome       = "/opt/solo/weaver"
	DefaultUnpackFolderName = "unpack"
	SystemBinDir            = "/usr/local/bin"
	SystemdUnitFilesDir     = "/usr/lib/systemd/system"
)

var (
	pp = NewWeaverPaths(DefaultWeaverHome)
)

func Paths() WeaverPaths {
	return *pp
}
