// SPDX-License-Identifier: Apache-2.0

package software

import (
	"path"
	"sort"
	"strings"

	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"

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
// compares them to the catalog checksums for the running platform. Unlike
// IsInstalled it hashes the bytes on disk rather than trusting cached state, so
// call it right before execution to catch post-install tampering.
//
// A recorded version that the catalog lists is authoritative: a mismatch is a
// ChecksumError even if the bytes match some other version. With no usable
// record (none, or one the catalog no longer lists) the binaries must match a
// catalog version by checksum; the catalog default is never assumed.
func VerifyExecutables(artifactName string) error {
	catalog, err := LoadInfrastructureCatalog()
	if err != nil {
		return NewConfigLoadError(err)
	}

	artifact, err := catalog.GetHostArtifact(artifactName)
	if err != nil {
		return err
	}

	recorded, err := readSoftwareVersion(artifact.Name)
	if err != nil {
		return err
	}

	return verifyInstalledExecutables(artifact, recorded, func(binaryName string) string {
		return installedBinaryPath(artifact.Name, binaryName)
	})
}

// verifyInstalledExecutables is the testable core of VerifyExecutables: it takes
// an explicit artifact and path resolver instead of the embedded catalog.
func verifyInstalledExecutables(artifact *ArtifactMetadata, recorded string, resolve func(binaryName string) string) error {
	if recorded != "" {
		if _, ok := artifact.Versions[Version(recorded)]; ok {
			return verifyArtifactExecutables(artifact, recorded, resolve)
		}
		logx.As().Warn().
			Str("artifact", artifact.Name).
			Str("version", recorded).
			Msg("Recorded version is not in the infrastructure catalog; identifying the installed binaries by checksum")
	}

	matches, err := identifyArtifactVersions(artifact, resolve)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		return newUnknownBinaryError(artifact, recorded)
	}

	logx.As().Debug().
		Str("artifact", artifact.Name).
		Strs("matchingVersions", matches).
		Msg("Installed binaries match the infrastructure catalog")
	return nil
}

// verifyArtifactExecutables checks every binary of one catalog version against
// that version's checksums.
func verifyArtifactExecutables(artifact *ArtifactMetadata, version string, resolve func(binaryName string) string) error {
	binaries, err := platformBinaries(artifact, version, resolve)
	if err != nil {
		return err
	}

	for _, b := range binaries {
		if err := VerifyChecksum(b.path, b.checksum, b.algorithm); err != nil {
			return err
		}
	}

	return nil
}

// IdentifyInstalledVersions hashes the named artifact's installed binaries and
// returns every catalog version they all match on this platform. An empty
// result means the binaries are present but match no known version. It reads
// nothing from state and runs nothing.
func IdentifyInstalledVersions(artifactName string) ([]string, error) {
	catalog, err := LoadInfrastructureCatalog()
	if err != nil {
		return nil, NewConfigLoadError(err)
	}

	artifact, err := catalog.GetHostArtifact(artifactName)
	if err != nil {
		return nil, err
	}

	return identifyArtifactVersions(artifact, func(binaryName string) string {
		return installedBinaryPath(artifact.Name, binaryName)
	})
}

// IsCatalogVersion reports whether the catalog lists version for the named host
// artifact.
func IsCatalogVersion(artifactName, version string) (bool, error) {
	catalog, err := LoadInfrastructureCatalog()
	if err != nil {
		return false, NewConfigLoadError(err)
	}

	artifact, err := catalog.GetHostArtifact(artifactName)
	if err != nil {
		return false, err
	}

	_, ok := artifact.Versions[Version(version)]
	return ok, nil
}

// identifyArtifactVersions returns the catalog versions whose binaries all match
// the installed files. Each file is hashed at most once per algorithm. It
// returns a FileNotFoundError when no version matched and at least one binary
// was missing for every candidate version, so an absent install is not
// reported as an unknown one.
func identifyArtifactVersions(artifact *ArtifactMetadata, resolve func(binaryName string) string) ([]string, error) {
	digests := map[string]string{}
	var missing error
	var matches []string
	candidates := 0
	completeCandidate := false

	for _, version := range sortedVersions(artifact) {
		binaries, err := platformBinaries(artifact, version, resolve)
		if err != nil {
			continue // this version cannot be verified on this platform
		}
		candidates++

		matched, complete := true, true
		for _, b := range binaries {
			key := b.algorithm + "\x00" + b.path
			digest, ok := digests[key]
			if !ok {
				digest, err = fileDigest(b.path, b.algorithm)
				if errorx.IsOfType(err, FileNotFoundError) {
					missing = err
					matched, complete = false, false
					break
				}
				if err != nil {
					return nil, err
				}
				digests[key] = digest
			}
			if digest != b.checksum {
				matched = false
				break
			}
		}

		if complete {
			completeCandidate = true
		}
		if matched {
			matches = append(matches, version)
		}
	}

	if candidates == 0 {
		platform := artifact.getPlatform()
		return nil, NewPlatformNotFoundError(artifact.Name, "any", platform.os, platform.arch)
	}
	if len(matches) == 0 && !completeCandidate && missing != nil {
		return nil, missing
	}

	return matches, nil
}

// platformBinary is one binary of a catalog version, resolved for the running
// platform.
type platformBinary struct {
	path      string
	algorithm string
	checksum  string
}

// platformBinaries resolves version's binaries and their checksums for the
// running platform. A version with no binaries, or with a binary that has no
// checksum for this platform, cannot be verified, so it is an error rather than
// something to skip.
func platformBinaries(artifact *ArtifactMetadata, version string, resolve func(binaryName string) string) ([]platformBinary, error) {
	versionInfo, exists := artifact.Versions[Version(version)]
	if !exists {
		return nil, NewVersionNotFoundError(artifact.Name, version)
	}

	platform := artifact.getPlatform()
	binaries := versionInfo.GetBinaries()
	if len(binaries) == 0 {
		return nil, NewPlatformNotFoundError(artifact.Name, version, platform.os, platform.arch)
	}

	data := TemplateData{VERSION: version, OS: platform.os, ARCH: platform.arch}
	result := make([]platformBinary, 0, len(binaries))
	for _, binary := range binaries {
		binaryName, err := executeTemplate(binary.Name, data)
		if err != nil {
			return nil, NewTemplateError(err, artifact.Name)
		}

		checksum, ok := binary.PlatformChecksum[platform.os][platform.arch]
		if !ok {
			return nil, NewPlatformNotFoundError(artifact.Name, version, platform.os, platform.arch)
		}

		result = append(result, platformBinary{
			path:      resolve(binaryName),
			algorithm: checksum.Algorithm,
			checksum:  checksum.Value,
		})
	}

	return result, nil
}

func sortedVersions(artifact *ArtifactMetadata) []string {
	versions := make([]string, 0, len(artifact.Versions))
	for v := range artifact.Versions {
		versions = append(versions, string(v))
	}
	sort.Strings(versions)
	return versions
}

// newUnknownBinaryError reports installed binaries that match no catalog
// version, so their integrity cannot be established.
func newUnknownBinaryError(artifact *ArtifactMetadata, recorded string) *errorx.Error {
	recordedText := recorded
	if recordedText == "" {
		recordedText = "none"
	}
	return ChecksumError.New(
		"installed %s binaries match no version in the infrastructure catalog (recorded version: %s; known versions: %s); refusing to run them",
		artifact.Name, recordedText, strings.Join(sortedVersions(artifact), ", ")).
		WithProperty(softwareNameProperty, artifact.Name)
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
