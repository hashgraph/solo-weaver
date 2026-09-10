// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"bytes"
	"os"
	"path/filepath"

	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
)

// infraVersionsManifestRel is the upgrade-package-relative path of the
// infrastructure-versions.yaml manifest (HIP-1494: manifests live under manifests/).
const infraVersionsManifestRel = "manifests/infrastructure-versions.yaml"

// defaultInfraVersionsPath is the trusted host location the manifest is placed at.
// Mirrors models.WeaverPaths.InfraVersionsPath; kept as a local constant so the
// daemon package stays self-contained. This file is meant to be overwritten on each
// upgrade — it is a destination, not a human-owned override.
const defaultInfraVersionsPath = "/opt/solo/weaver/config/infrastructure-versions.yaml"

// infraVersionsBackupSuffix names the single rolling backup of the previous
// trusted-location file, written before an overwrite so the operator can diff or
// restore the immediately-prior manifest. Only the last version is kept — the full
// history lives in retained upgrade packages and the in-cluster config CRs.
const infraVersionsBackupSuffix = ".bak"

// placeInfraVersions copies the upgrade package's infrastructure-versions.yaml to the
// trusted host location destPath, overwriting whatever is there. When the package
// carries no such manifest it is a no-op — the upgrade declares no infra-version
// constraint and the embedded catalog's defaults apply. Idempotent: the write is
// skipped when the destination already holds identical bytes. Placement only moves
// bytes; validation against the catalog is the runtime safety gate's job.
func placeInfraVersions(srcPkgDir, destPath string) error {
	src := filepath.Join(srcPkgDir, infraVersionsManifestRel)

	data, present, err := readInfraVersionsSource(src)
	if err != nil {
		return err
	}
	if !present {
		logx.As().Info().Str("reason", ReasonInfraVersionsAbsent.String()).Str("path", src).
			Msg("no infrastructure-versions.yaml in upgrade package — no infra-version constraint")
		return nil
	}

	existing, existErr := os.ReadFile(destPath)
	if existErr == nil && bytes.Equal(existing, data) {
		logx.As().Debug().Str("reason", ReasonInfraVersionsPlaced.String()).Str("path", destPath).
			Msg("infrastructure-versions.yaml already placed with matching content — skipping")
		return nil
	}

	// Back up the previous file before overwriting — but only when it exists and
	// differs (we already returned above on an identical match), so the single
	// rolling .bak never churns on a no-op re-run.
	if existErr == nil {
		if err := atomicWriteFile(destPath+infraVersionsBackupSuffix, existing); err != nil {
			return err
		}
		logx.As().Info().Str("reason", ReasonInfraVersionsBackedUp.String()).
			Str("path", destPath+infraVersionsBackupSuffix).
			Msg("backed up previous infrastructure-versions.yaml before overwrite")
	}

	if err := atomicWriteFile(destPath, data); err != nil {
		return err
	}
	logx.As().Info().Str("reason", ReasonInfraVersionsPlaced.String()).Str("path", destPath).
		Msg("placed infrastructure-versions.yaml at trusted location")
	return nil
}

// readInfraVersionsSource reads the package manifest with the same symlink/oversize
// rejection as config files. Returns (nil, false, nil) when the file is absent.
func readInfraVersionsSource(src string) ([]byte, bool, error) {
	info, err := os.Lstat(src) // not Stat: don't follow a symlink
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, errx.WithHints(
			errx.WithReason(ErrUpgradePath.Wrap(err, "stat %s", src), ReasonUpgradePathUnreadable),
			"Confirm the upgrade package was extracted correctly and manifests/ is readable",
		)
	}
	if err := rejectSymlink(src, info.Mode()); err != nil {
		return nil, false, err
	}
	if err := rejectOversized(src, info.Size()); err != nil {
		return nil, false, err
	}
	data, err := readFileNoFollow(src)
	if err != nil {
		if isDecorated(err) {
			return nil, false, err
		}
		return nil, false, errx.WithHints(
			errx.WithReason(ErrUpgradePath.Wrap(err, "read %s", src), ReasonUpgradePathUnreadable),
			"Confirm the upgrade package was extracted correctly and the manifest is readable",
		)
	}
	return data, true, nil
}

// atomicWriteFile writes data to path via a temp file in the same directory then
// renames, so a reader never sees a partial file. Creates the destination directory
// if absent. I/O failures are transient (retried via re-delivery until the deadline).
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errx.WithHints(
			errx.WithReason(ErrInfraVersionsPlace.Wrap(err, "create %s", dir), ReasonInfraVersionsPlaceFailed),
			"Confirm the daemon can write to the trusted config directory",
		)
	}
	tmp, err := os.CreateTemp(dir, ".infra-versions-*.tmp")
	if err != nil {
		return errx.WithHints(
			errx.WithReason(ErrInfraVersionsPlace.Wrap(err, "create temp file in %s", dir), ReasonInfraVersionsPlaceFailed),
			"Confirm the daemon can write to the trusted config directory",
		)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return errx.WithReason(ErrInfraVersionsPlace.Wrap(err, "write %s", tmpName), ReasonInfraVersionsPlaceFailed)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return errx.WithReason(ErrInfraVersionsPlace.Wrap(err, "chmod %s", tmpName), ReasonInfraVersionsPlaceFailed)
	}
	if err := tmp.Close(); err != nil {
		return errx.WithReason(ErrInfraVersionsPlace.Wrap(err, "close %s", tmpName), ReasonInfraVersionsPlaceFailed)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return errx.WithReason(ErrInfraVersionsPlace.Wrap(err, "rename %s to %s", tmpName, path), ReasonInfraVersionsPlaceFailed)
	}
	return nil
}
