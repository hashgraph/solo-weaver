// SPDX-License-Identifier: Apache-2.0

package software

import (
	"os"
	"path"

	"github.com/hashgraph/solo-weaver/pkg/codesign"
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

// Download obtains the daemon binary. For a signed-release catalog entry it
// resolves the versioned release URL, downloads the binary and its detached
// signature, and verifies the signature against the embedded release key
// (pkg/codesign) before the binary is eligible to install. Otherwise it falls
// back to the checksum-based base installer download.
func (d *daemonInstaller) Download() error {
	if d.software.SignedRelease == nil {
		return d.baseInstaller.Download()
	}
	return d.downloadSignedRelease()
}

func (d *daemonInstaller) downloadSignedRelease() error {
	spec := d.software.SignedRelease
	platform := d.software.getPlatform()
	data := TemplateData{VERSION: d.versionToBeInstalled, OS: platform.os, ARCH: platform.arch}

	binURL, err := executeTemplate(spec.URL, data)
	if err != nil {
		return NewTemplateError(err, d.software.Name)
	}
	sigURL := binURL + spec.SigURLSuffix()

	downloadsDir := models.Paths().DownloadsDir
	if err := d.fileManager.CreateDirectory(downloadsDir, true); err != nil {
		return NewDownloadError(err, downloadsDir, 0)
	}

	binPath := path.Join(downloadsDir, path.Base(binURL))
	sigPath := binPath + spec.SigURLSuffix()

	if err := d.downloader.Download(binURL, binPath); err != nil {
		return err
	}

	// Once the binary is on disk, any subsequent failure (signature download or
	// verification) must remove both files: Install() does not re-verify, so an
	// unverified binary left behind could be picked up by a later run.
	if err := d.downloader.Download(sigURL, sigPath); err != nil {
		d.removeSignedReleaseDownload(binPath, sigPath)
		return err
	}
	if err := verifyReleaseSignature(binPath, sigPath); err != nil {
		d.removeSignedReleaseDownload(binPath, sigPath)
		return err
	}
	return nil
}

// removeSignedReleaseDownload discards a partially-complete or unverified
// signed-release download so it can never be installed.
func (d *daemonInstaller) removeSignedReleaseDownload(binPath, sigPath string) {
	_ = d.fileManager.RemoveAll(binPath)
	_ = d.fileManager.RemoveAll(sigPath)
}

// verifyReleaseSignature checks the downloaded binary against its detached
// signature using the embedded release key.
func verifyReleaseSignature(binPath, sigPath string) error {
	bin, err := os.Open(binPath)
	if err != nil {
		return errorx.ExternalError.Wrap(err, "failed to open downloaded daemon binary %s", binPath)
	}
	defer func() { _ = bin.Close() }()

	sig, err := os.Open(sigPath)
	if err != nil {
		return errorx.ExternalError.Wrap(err, "failed to open daemon signature %s", sigPath)
	}
	defer func() { _ = sig.Close() }()

	return codesign.Verify(bin, sig)
}

// Install copies the downloaded daemon binary to paths.BinDir instead of
// SandboxBinDir. The service unit hardcodes ExecStart=/opt/solo/weaver/bin/solo-provisioner-daemon
// so the binary must live there, not in the sandbox.
func (d *daemonInstaller) Install() error {
	binDir := models.Paths().BinDir
	if err := d.fileManager.CreateDirectory(binDir, true); err != nil {
		return NewInstallationError(err, "", binDir)
	}

	if d.software.SignedRelease != nil {
		return d.installSignedRelease(binDir)
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

// installSignedRelease copies the verified, downloaded daemon binary to binDir
// under the configured binary name (the name the service unit's ExecStart
// expects), regardless of the versioned download filename.
func (d *daemonInstaller) installSignedRelease(binDir string) error {
	spec := d.software.SignedRelease
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
