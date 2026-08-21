// SPDX-License-Identifier: Apache-2.0

package software

import (
	"path"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

const DaemonBinaryName = "solo-provisioner-daemon"

type daemonInstaller struct {
	*baseInstaller
}

// NewDaemonInstaller creates an installer for the solo-provisioner-daemon binary.
// It follows the same pattern as other host software installers but overrides
// Install so the binary lands at paths.BinDir (the path hardcoded in the service
// unit's ExecStart) rather than the generic SandboxBinDir.
func NewDaemonInstaller(opts ...InstallerOption) (Software, error) {
	bi, err := newBaseInstaller(DaemonBinaryName, opts...)
	if err != nil {
		return nil, err
	}

	di := &daemonInstaller{baseInstaller: bi}

	// verifyInstalled checks paths.BinDir, not SandboxBinDir.
	di.baseInstaller.verifyInstalled = di.verifyDaemonBinary

	return di, nil
}

// Download obtains the daemon binary. For a self-released catalog entry it
// resolves the versioned release URL and downloads the binary, accepting it only
// if its digest matches the one resolveExpectedDigest establishes. Otherwise it
// falls back to the checksum-based base installer download.
func (d *daemonInstaller) Download() error {
	if d.software.SelfRelease == nil {
		return d.baseInstaller.Download()
	}
	return d.downloadSelfRelease()
}

func (d *daemonInstaller) downloadSelfRelease() error {
	spec := d.software.SelfRelease
	platform := d.software.getPlatform()
	data := TemplateData{VERSION: d.versionToBeInstalled, OS: platform.os, ARCH: platform.arch}

	binURL, err := executeTemplate(spec.URL, data)
	if err != nil {
		return NewTemplateError(err, d.software.Name)
	}

	downloadsDir := models.Paths().DownloadsDir
	if err := d.fileManager.CreateDirectory(downloadsDir, true); err != nil {
		return NewDownloadError(err, downloadsDir, 0)
	}

	// Resolve the expected digest before fetching the binary. Install() does not
	// re-verify, so a binary must never reach the downloads dir while there is
	// still no digest to hold it to.
	expected, err := d.resolveExpectedDigest(spec, binURL, downloadsDir)
	if err != nil {
		return err
	}

	// DownloadAndVerify removes the file on a mismatch, so a rejected artifact is
	// never left behind for a later run to install unchecked.
	binPath := path.Join(downloadsDir, path.Base(binURL))
	return d.downloader.DownloadAndVerify(binURL, binPath, expected, daemonChecksumAlgorithm)
}

// resolveExpectedDigest returns the digest the downloaded daemon binary must match.
//
// For the co-released version — the default, since --daemon-version defaults to
// this binary's own version — that is the digest stamped in at link time: an
// anchor produced by the same pipeline run, needing no network fetch and no
// signing key. Any other version can only have been named by an explicit
// --daemon-version, and is held to its own published checksum asset fetched over
// TLS, the same trust install.sh uses to place the CLI on the host to begin with.
func (d *daemonInstaller) resolveExpectedDigest(spec *SelfReleaseSpec, binURL, downloadsDir string) (string, error) {
	if digest, ok := pinnedDigestFor(d.versionToBeInstalled); ok {
		return digest, nil
	}

	checksumURL := binURL + spec.ChecksumSuffix()
	checksumPath := path.Join(downloadsDir, path.Base(checksumURL))
	if err := d.downloader.Download(checksumURL, checksumPath); err != nil {
		return "", err
	}
	// The asset is consumed here and nothing downstream reads it, so it is not
	// left in the downloads dir to be mistaken for a verified input.
	defer func() { _ = d.fileManager.RemoveAll(checksumPath) }()

	content, err := d.fileManager.ReadFile(checksumPath, maxChecksumAssetBytes)
	if err != nil {
		return "", errorx.ExternalError.Wrap(err,
			"failed to read the downloaded checksum asset %s", checksumPath)
	}
	return parseChecksumAsset(content, path.Base(checksumURL))
}

// Install copies the downloaded daemon binary to paths.BinDir instead of
// SandboxBinDir. The service unit hardcodes ExecStart=/opt/solo/weaver/bin/solo-provisioner-daemon
// so the binary must live there, not in the sandbox.
func (d *daemonInstaller) Install() error {
	binDir := models.Paths().BinDir
	if err := d.fileManager.CreateDirectory(binDir, true); err != nil {
		return NewInstallationError(err, "", binDir)
	}

	if d.software.SelfRelease != nil {
		return d.installSelfRelease(binDir)
	}

	versionInfo, exists := d.software.Versions[Version(d.versionToBeInstalled)]
	if !exists {
		return NewVersionNotFoundError(d.software.Name, d.versionToBeInstalled)
	}

	platform := d.software.getPlatform()
	data := TemplateData{
		VERSION: d.versionToBeInstalled,
		OS:      platform.os,
		ARCH:    platform.arch,
	}

	downloadFolder := models.Paths().DownloadsDir

	for _, binary := range versionInfo.BinariesByURL() {
		binaryName, err := executeTemplate(binary.Name, data)
		if err != nil {
			return NewTemplateError(err, d.software.Name)
		}

		srcPath := path.Join(downloadFolder, path.Base(binaryName))
		dstPath := path.Join(binDir, path.Base(binaryName))

		_, exists, err := d.fileManager.PathExists(srcPath)
		if err != nil || !exists {
			return NewFileNotFoundError(srcPath)
		}

		if err := d.installFile(srcPath, dstPath, models.DefaultDirOrExecPerm); err != nil {
			return NewInstallationError(err, srcPath, binDir)
		}
	}

	return d.recordInstalled()
}

// installSelfRelease copies the verified, downloaded daemon binary to binDir
// under the configured binary name (the name the service unit's ExecStart
// expects), regardless of the versioned download filename.
func (d *daemonInstaller) installSelfRelease(binDir string) error {
	spec := d.software.SelfRelease
	platform := d.software.getPlatform()
	data := TemplateData{VERSION: d.versionToBeInstalled, OS: platform.os, ARCH: platform.arch}

	binURL, err := executeTemplate(spec.URL, data)
	if err != nil {
		return NewTemplateError(err, d.software.Name)
	}
	binaryName, err := executeTemplate(spec.BinaryName, data)
	if err != nil {
		return NewTemplateError(err, d.software.Name)
	}

	srcPath := path.Join(models.Paths().DownloadsDir, path.Base(binURL))
	dstPath := path.Join(binDir, binaryName)

	_, exists, err := d.fileManager.PathExists(srcPath)
	if err != nil || !exists {
		return NewFileNotFoundError(srcPath)
	}

	if err := d.installFile(srcPath, dstPath, models.DefaultDirOrExecPerm); err != nil {
		return NewInstallationError(err, srcPath, binDir)
	}

	return d.recordInstalled()
}

// Uninstall removes the daemon binary from paths.BinDir.
func (d *daemonInstaller) Uninstall() error {
	binPath := path.Join(models.Paths().BinDir, DaemonBinaryName)
	_ = d.fileManager.RemoveAll(binPath)
	_ = d.clearInstalled()
	return nil
}

// Configure is a no-op: the binary lives at its final location (paths.BinDir)
// after Install, so no symlink to /usr/local/bin is needed.
func (d *daemonInstaller) Configure() error {
	return d.recordConfigured()
}

// RemoveConfiguration is a no-op — no symlinks were created by Configure.
func (d *daemonInstaller) RemoveConfiguration() error {
	return d.clearConfigured()
}

// verifyDaemonBinary checks that the daemon binary exists at paths.BinDir.
func (d *daemonInstaller) verifyDaemonBinary() error {
	binPath := path.Join(models.Paths().BinDir, DaemonBinaryName)
	_, exists, err := d.fileManager.PathExists(binPath)
	if err != nil {
		return NewFileNotFoundError(binPath)
	}
	if !exists {
		return NewFileNotFoundError(binPath)
	}
	return nil
}
