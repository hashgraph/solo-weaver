// SPDX-License-Identifier: Apache-2.0

package models

const (
	// File and directory permissions
	DefaultDirOrExecPerm = 0755 // for directories and executable files
	DefaultFilePerm      = 0644 // for regular data/config files

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
