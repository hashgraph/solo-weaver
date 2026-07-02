// SPDX-License-Identifier: Apache-2.0

package software

import (
	"path"

	"github.com/automa-saga/logx"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// readSoftwareVersion reads the recorded version of an installed component from
// on-disk state. It is a package var so tests can stub it without a state file.
var readSoftwareVersion = state.ReadSoftwareVersionFromDisk

// verifyExecutables is the time-of-use checksum check invoked by in-package
// installers (e.g. the teleport node agent) right before executing a binary. It
// is a package var so tests can stub the verification result without a real
// catalog or on-disk binaries.
var verifyExecutables = VerifyExecutables

// VerifyExecutables re-hashes the named artifact's installed binaries and
// compares them to the catalog checksum for the running platform, returning a
// ChecksumError on mismatch. Unlike IsInstalled it hashes the bytes on disk
// rather than trusting cached state, so call it right before execution to catch
// post-install tampering.
//
// Checksums are resolved for the version recorded in on-disk state, not the
// catalog default, so the check stays correct after a default bump leaves an
// older binary in place. It falls back to the catalog default when state has no
// record (e.g. a fresh install verified within the same workflow).
func VerifyExecutables(artifactName string) error {
	catalog, err := LoadInfrastructureCatalog()
	if err != nil {
		return NewConfigLoadError(err)
	}

	artifact, err := catalog.GetHostArtifact(artifactName)
	if err != nil {
		return err
	}

	version, err := resolveInstalledVersion(artifact)
	if err != nil {
		return err
	}

	// With no catalog checksum for the installed version there is nothing to
	// compare against. Skip rather than fail closed, which would block every
	// host still running a now-delisted version.
	if _, ok := artifact.Versions[Version(version)]; !ok {
		logx.As().Warn().
			Str("artifact", artifactName).
			Str("version", version).
			Msg("Installed version is not present in the infrastructure catalog; skipping checksum verification")
		return nil
	}

	return verifyArtifactExecutables(artifact, version, func(binaryName string) string {
		return installedBinaryPath(artifact.Name, binaryName)
	})
}

// resolveInstalledVersion returns the version recorded for the artifact in
// on-disk state, falling back to the catalog default when state has no entry.
func resolveInstalledVersion(artifact *ArtifactMetadata) (string, error) {
	recorded, err := readSoftwareVersion(artifact.Name)
	if err != nil {
		return "", err
	}
	if recorded != "" {
		return recorded, nil
	}
	return artifact.GetDefaultVersion()
}

// verifyArtifactExecutables is the testable core of VerifyExecutables: it takes
// an explicit artifact and path resolver instead of the embedded catalog.
func verifyArtifactExecutables(artifact *ArtifactMetadata, version string, resolve func(binaryName string) string) error {
	versionInfo, exists := artifact.Versions[Version(version)]
	if !exists {
		return NewVersionNotFoundError(artifact.Name, version)
	}

	platform := artifact.getPlatform()
	data := TemplateData{VERSION: version, OS: platform.os, ARCH: platform.arch}

	for _, binary := range versionInfo.GetBinaries() {
		binaryName, err := executeTemplate(binary.Name, data)
		if err != nil {
			return NewTemplateError(err, artifact.Name)
		}

		// Skip binaries with no checksum for this OS/arch rather than failing.
		osInfo, ok := binary.PlatformChecksum[platform.os]
		if !ok {
			continue
		}
		checksum, ok := osInfo[platform.arch]
		if !ok {
			continue
		}

		if err := VerifyChecksum(resolve(binaryName), checksum.Value, checksum.Algorithm); err != nil {
			return err
		}
	}

	return nil
}

// installedBinaryPath maps a catalog binary name to its sandbox path. Most
// artifacts install flat under SandboxBinDir; cri-o uses crioInstalledBinaryPaths.
func installedBinaryPath(artifactName, binaryName string) string {
	base := path.Base(binaryName)

	if artifactName == CrioArtifactName {
		if p, ok := crioInstalledBinaryPaths(models.Paths().SandboxDir)[base]; ok {
			return p
		}
	}

	return path.Join(models.Paths().SandboxBinDir, base)
}
