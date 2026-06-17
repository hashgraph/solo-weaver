// SPDX-License-Identifier: Apache-2.0

package selfupgrade

import (
	"path/filepath"
	"strings"

	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
)

// Binary identifies which solo-provisioner binary a .bak archive belongs to.
type Binary string

const (
	// BinaryCLI is the operator-facing CLI binary, solo-provisioner.
	BinaryCLI Binary = "solo-provisioner"

	// BinaryDaemon is the long-running daemon binary, solo-provisioner-daemon.
	BinaryDaemon Binary = "solo-provisioner-daemon"

	// bakExtension is the suffix on every archived binary filename.
	bakExtension = ".bak"
)

// .bak naming convention (shared by archive #526 and recover #528/#717):
//
//	solo-provisioner-<operationId>.bak           — archived CLI binary
//	solo-provisioner-daemon-<operationId>.bak    — archived daemon binary
//
// The live binaries are installed in WeaverPaths.BinDir (/opt/solo/weaver/bin),
// which is root:root. Their archives are NOT kept there: they live in a
// dedicated backup subdirectory, BakDir = /opt/solo/weaver/backup/solo-provisioner
// (under WeaverPaths.BackupDir). This keeps the bin dir to live executables only
// and the backup tree to archives. Both are under /opt/solo/weaver, i.e. the
// same filesystem as the live binary, so the swap's rename stays atomic. The
// self-upgrade swap runs as root, so writing into either tree is fine.
//
// Note the daemon name embeds the CLI name as a prefix, so any parser MUST test
// the daemon prefix first. ParseBakName does.

// bakSubdir is the dedicated subdirectory under WeaverPaths.BackupDir that holds
// archived solo-provisioner binaries.
const bakSubdir = "solo-provisioner"

// BakDir returns the directory archived binaries live in, given the weaver
// backup directory (WeaverPaths.BackupDir): backupDir/solo-provisioner.
func BakDir(backupDir string) string {
	return filepath.Join(backupDir, bakSubdir)
}

// CLIBakName returns the .bak filename for the CLI binary archived under
// operationID. It rejects an operationID that is not path-safe (sanity
// .ValidateOperationID) so the id can never introduce a path separator or ".."
// traversal sequence into the filename.
func CLIBakName(operationID string) (string, error) {
	if err := sanity.ValidateOperationID(operationID); err != nil {
		return "", err
	}
	return string(BinaryCLI) + "-" + operationID + bakExtension, nil
}

// DaemonBakName returns the .bak filename for the daemon binary archived under
// operationID, with the same validation as CLIBakName.
func DaemonBakName(operationID string) (string, error) {
	if err := sanity.ValidateOperationID(operationID); err != nil {
		return "", err
	}
	return string(BinaryDaemon) + "-" + operationID + bakExtension, nil
}

// CLIBakPath returns the absolute .bak path for the CLI binary in bakDir
// (typically BakDir(WeaverPaths.BackupDir)). The operationID is validated, so a
// crafted id cannot escape bakDir.
func CLIBakPath(bakDir, operationID string) (string, error) {
	name, err := CLIBakName(operationID)
	if err != nil {
		return "", err
	}
	return filepath.Join(bakDir, name), nil
}

// DaemonBakPath returns the absolute .bak path for the daemon binary in bakDir
// (typically BakDir(WeaverPaths.BackupDir)), with the same validation as CLIBakPath.
func DaemonBakPath(bakDir, operationID string) (string, error) {
	name, err := DaemonBakName(operationID)
	if err != nil {
		return "", err
	}
	return filepath.Join(bakDir, name), nil
}

// ParseBakName parses a .bak filename produced by CLIBakName/DaemonBakName,
// returning which binary it archives and the embedded operationId. It accepts a
// bare filename or a path (only the base name is parsed). The daemon prefix is
// tested before the CLI prefix because the daemon name contains the CLI name.
func ParseBakName(name string) (Binary, string, error) {
	base := filepath.Base(name)

	stem, ok := strings.CutSuffix(base, bakExtension)
	if !ok {
		return "", "", errorx.IllegalFormat.New("%q is not a .bak archive (missing %s suffix)", base, bakExtension)
	}

	// Daemon first: "solo-provisioner-daemon-" extends "solo-provisioner-".
	if opID, ok := strings.CutPrefix(stem, string(BinaryDaemon)+"-"); ok {
		if opID == "" {
			return "", "", errorx.IllegalFormat.New("%q has an empty operationId", base)
		}
		return BinaryDaemon, opID, nil
	}
	if opID, ok := strings.CutPrefix(stem, string(BinaryCLI)+"-"); ok {
		if opID == "" {
			return "", "", errorx.IllegalFormat.New("%q has an empty operationId", base)
		}
		return BinaryCLI, opID, nil
	}

	return "", "", errorx.IllegalFormat.New(
		"%q does not match the %s-<operationId>%s or %s-<operationId>%s pattern",
		base, BinaryCLI, bakExtension, BinaryDaemon, bakExtension)
}
