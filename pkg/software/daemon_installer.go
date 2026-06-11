// SPDX-License-Identifier: Apache-2.0

package software

import (
	"path"

	"github.com/hashgraph/solo-weaver/pkg/models"
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

// Install copies the downloaded daemon binary to paths.BinDir instead of
// SandboxBinDir. The service unit hardcodes ExecStart=/opt/solo/weaver/bin/solo-provisioner-daemon
// so the binary must live there, not in the sandbox.
func (d *daemonInstaller) Install() error {
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

	binDir := models.Paths().BinDir
	if err := d.fileManager.CreateDirectory(binDir, true); err != nil {
		return NewInstallationError(err, "", binDir)
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
