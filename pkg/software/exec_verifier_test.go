// SPDX-License-Identifier: Apache-2.0

package software

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTempBinary writes content to a fresh temp file and returns its path plus
// the lowercase hex sha256 of that content.
func writeTempBinary(t *testing.T, content string) (string, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "binary")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o755))
	sum := sha256.Sum256([]byte(content))
	return p, hex.EncodeToString(sum[:])
}

// newTestArtifact builds a synthetic single-version host artifact pinned to a
// platform, so verifyArtifactExecutables runs without the embedded catalog.
func newTestArtifact(osName, arch string, binaries ...BinaryDetail) *ArtifactMetadata {
	return (&ArtifactMetadata{
		Name:    "test-artifact",
		Default: "1.0.0",
		Versions: map[Version]VersionDetails{
			"1.0.0": {Binaries: binaries},
		},
	}).withPlatform(osName, arch)
}

func linuxAmd64Checksum(algorithm, value string) PlatformChecksum {
	return PlatformChecksum{
		"linux": {"amd64": {Algorithm: algorithm, Value: value}},
	}
}

func TestVerifyArtifactExecutables_CleanPasses(t *testing.T) {
	binPath, sum := writeTempBinary(t, "trusted-binary-bytes")

	artifact := newTestArtifact("linux", "amd64", BinaryDetail{
		Name:             "tool",
		PlatformChecksum: linuxAmd64Checksum("sha256", sum),
	})

	err := verifyArtifactExecutables(artifact, "1.0.0", func(string) string { return binPath })
	require.NoError(t, err)
}

func TestVerifyArtifactExecutables_TamperedFails(t *testing.T) {
	binPath, _ := writeTempBinary(t, "tampered-binary-bytes")
	_, expectedSum := writeTempBinary(t, "the-original-bytes")

	artifact := newTestArtifact("linux", "amd64", BinaryDetail{
		Name:             "tool",
		PlatformChecksum: linuxAmd64Checksum("sha256", expectedSum),
	})

	err := verifyArtifactExecutables(artifact, "1.0.0", func(string) string { return binPath })
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, ChecksumError), "expected ChecksumError, got %v", err)
}

func TestVerifyArtifactExecutables_MissingFileFails(t *testing.T) {
	artifact := newTestArtifact("linux", "amd64", BinaryDetail{
		Name:             "tool",
		PlatformChecksum: linuxAmd64Checksum("sha256", "deadbeef"),
	})

	missing := filepath.Join(t.TempDir(), "does-not-exist")
	err := verifyArtifactExecutables(artifact, "1.0.0", func(string) string { return missing })
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, FileNotFoundError), "expected FileNotFoundError, got %v", err)
}

func TestVerifyArtifactExecutables_SkipsUnlistedPlatform(t *testing.T) {
	// linux/amd64-only checksum but running platform is darwin/arm64 → skipped,
	// resolver never called.
	artifact := newTestArtifact("darwin", "arm64", BinaryDetail{
		Name:             "tool",
		PlatformChecksum: linuxAmd64Checksum("sha256", "deadbeef"),
	})

	resolved := false
	err := verifyArtifactExecutables(artifact, "1.0.0", func(string) string {
		resolved = true
		return "/nonexistent"
	})
	require.NoError(t, err)
	require.False(t, resolved, "resolver must not be called for an unlisted platform")
}

func TestVerifyArtifactExecutables_VerifiesAllBinaries(t *testing.T) {
	goodPath, goodSum := writeTempBinary(t, "good-bytes")
	badPath, _ := writeTempBinary(t, "actual-bad-bytes")
	_, expectedBadSum := writeTempBinary(t, "expected-bad-bytes")

	artifact := newTestArtifact("linux", "amd64",
		BinaryDetail{Name: "good", PlatformChecksum: linuxAmd64Checksum("sha256", goodSum)},
		BinaryDetail{Name: "bad", PlatformChecksum: linuxAmd64Checksum("sha256", expectedBadSum)},
	)

	paths := map[string]string{"good": goodPath, "bad": badPath}
	err := verifyArtifactExecutables(artifact, "1.0.0", func(name string) string { return paths[name] })
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, ChecksumError), "expected ChecksumError from the tampered binary, got %v", err)
}

func TestVerifyArtifactExecutables_UnknownVersion(t *testing.T) {
	artifact := newTestArtifact("linux", "amd64", BinaryDetail{Name: "tool"})
	err := verifyArtifactExecutables(artifact, "9.9.9", func(string) string { return "" })
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, VersionNotFoundError), "expected VersionNotFoundError, got %v", err)
}

func TestInstalledBinaryPath_Generic(t *testing.T) {
	// Non-crio artifacts install flat under SandboxBinDir by basename.
	require.Equal(t,
		path.Join(models.Paths().SandboxBinDir, "cilium"),
		installedBinaryPath("cilium", "cilium"))

	// teleport's catalog name carries a directory prefix that is stripped.
	require.Equal(t,
		path.Join(models.Paths().SandboxBinDir, "teleport"),
		installedBinaryPath("teleport", "teleport-ent/teleport"))
}

func TestInstalledBinaryPath_Crio(t *testing.T) {
	sandboxDir := models.Paths().SandboxDir

	// Runtime helpers relocate under libexec/crio.
	require.Equal(t,
		filepath.Join(sandboxDir, libexecCrioDir, "crun"),
		installedBinaryPath(CrioArtifactName, "cri-o/bin/crun"))

	// crio/pinns/crictl relocate under the local bin dir.
	require.Equal(t,
		filepath.Join(sandboxDir, binDir, "crio"),
		installedBinaryPath(CrioArtifactName, "cri-o/bin/crio"))

	// A binary not present in the crio map falls back to the generic location.
	require.Equal(t,
		path.Join(models.Paths().SandboxBinDir, "unknown"),
		installedBinaryPath(CrioArtifactName, "cri-o/bin/unknown"))
}

func TestVerifyExecutables_UnknownArtifact(t *testing.T) {
	err := VerifyExecutables("no-such-artifact")
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, SoftwareNotFoundError), "expected SoftwareNotFoundError, got %v", err)
}

// stubReadSoftwareVersion swaps the state-version reader for the duration of a
// test and restores it on cleanup.
func stubReadSoftwareVersion(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	orig := readSoftwareVersion
	readSoftwareVersion = fn
	t.Cleanup(func() { readSoftwareVersion = orig })
}

func TestVerifyExecutables_SkipsWhenRecordedVersionNotInCatalog(t *testing.T) {
	// A host running a version the catalog no longer lists cannot be verified;
	// verification must be skipped (nil error) rather than failing closed.
	stubReadSoftwareVersion(t, func(string) (string, error) { return "0.0.0-delisted", nil })

	// "cilium" is a real host artifact, so resolution reaches the version guard.
	err := VerifyExecutables("cilium")
	require.NoError(t, err, "verification must be skipped, not fail closed, for a delisted version")
}

func TestVerifyExecutables_PropagatesStateReadError(t *testing.T) {
	stubReadSoftwareVersion(t, func(string) (string, error) { return "", assert.AnError })

	err := VerifyExecutables("cilium")
	require.ErrorIs(t, err, assert.AnError)
}
