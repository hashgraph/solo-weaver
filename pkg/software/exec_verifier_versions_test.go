// SPDX-License-Identifier: Apache-2.0

package software

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	olderVersion   = "1.33.4" // installed by an earlier provisioner
	defaultVersion = "1.36.5" // the running provisioner's catalog default
)

// upgradedCatalogArtifact models a catalog whose default moved to 1.36.5 while
// still listing the 1.33.4 an existing host runs.
func upgradedCatalogArtifact(olderSum, defaultSum string) *ArtifactMetadata {
	return (&ArtifactMetadata{
		Name:    "kubelet",
		Default: defaultVersion,
		Versions: map[Version]VersionDetails{
			olderVersion:   {Binaries: []BinaryDetail{{Name: "kubelet", PlatformChecksum: linuxAmd64Checksum("sha256", olderSum)}}},
			defaultVersion: {Binaries: []BinaryDetail{{Name: "kubelet", PlatformChecksum: linuxAmd64Checksum("sha256", defaultSum)}}},
		},
	}).withPlatform("linux", "amd64")
}

// installedBinary writes the bytes of one release and returns a resolver
// pointing at it, plus the sha256 of each release's bytes.
func installedBinary(t *testing.T, content string) (func(string) string, string, string) {
	t.Helper()
	path, _ := writeTempBinary(t, content)
	_, olderSum := writeTempBinary(t, "kubelet-1.33.4")
	_, defaultSum := writeTempBinary(t, "kubelet-1.36.5")
	return func(string) string { return path }, olderSum, defaultSum
}

func TestVerifyInstalledExecutables_RecordedOlderVersionPasses(t *testing.T) {
	resolve, olderSum, defaultSum := installedBinary(t, "kubelet-1.33.4")
	err := verifyInstalledExecutables(upgradedCatalogArtifact(olderSum, defaultSum), olderVersion, resolve)
	require.NoError(t, err)
}

func TestVerifyInstalledExecutables_RecordedVersionMismatchFails(t *testing.T) {
	// The bytes are a genuine 1.36.5, but the record says 1.33.4. That is an
	// integrity failure, not a cue to follow the bytes to another version.
	resolve, olderSum, defaultSum := installedBinary(t, "kubelet-1.36.5")
	err := verifyInstalledExecutables(upgradedCatalogArtifact(olderSum, defaultSum), olderVersion, resolve)
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, ChecksumError), "expected ChecksumError, got %v", err)
}

func TestVerifyInstalledExecutables_UnrecordedOlderBinaryIsIdentified(t *testing.T) {
	resolve, olderSum, defaultSum := installedBinary(t, "kubelet-1.33.4")
	err := verifyInstalledExecutables(upgradedCatalogArtifact(olderSum, defaultSum), "", resolve)
	require.NoError(t, err, "a known older binary must pass without a record, not be checked against the default")
}

func TestVerifyInstalledExecutables_DelistedRecord(t *testing.T) {
	t.Run("identified", func(t *testing.T) {
		resolve, olderSum, defaultSum := installedBinary(t, "kubelet-1.33.4")
		err := verifyInstalledExecutables(upgradedCatalogArtifact(olderSum, defaultSum), "1.30.0", resolve)
		require.NoError(t, err)
	})
	t.Run("unknown bytes fail", func(t *testing.T) {
		resolve, olderSum, defaultSum := installedBinary(t, "kubelet-1.30.0")
		err := verifyInstalledExecutables(upgradedCatalogArtifact(olderSum, defaultSum), "1.30.0", resolve)
		require.Error(t, err, "a delisted record must not skip verification")
		assert.True(t, errorx.IsOfType(err, ChecksumError), "expected ChecksumError, got %v", err)
	})
}

func TestVerifyInstalledExecutables_UnknownBinaryFails(t *testing.T) {
	resolve, olderSum, defaultSum := installedBinary(t, "tampered-bytes")
	err := verifyInstalledExecutables(upgradedCatalogArtifact(olderSum, defaultSum), "", resolve)
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, ChecksumError), "expected ChecksumError, got %v", err)
	assert.Contains(t, err.Error(), "match no version in the infrastructure catalog")
	assert.Contains(t, err.Error(), "1.33.4, 1.36.5")
}

func TestVerifyInstalledExecutables_AmbiguousMatchPasses(t *testing.T) {
	// Two catalog versions with identical bytes: integrity holds even though the
	// version cannot be told apart.
	resolve, olderSum, _ := installedBinary(t, "kubelet-1.33.4")
	err := verifyInstalledExecutables(upgradedCatalogArtifact(olderSum, olderSum), "", resolve)
	require.NoError(t, err)
}

func TestVerifyInstalledExecutables_MissingBinary(t *testing.T) {
	_, olderSum, defaultSum := installedBinary(t, "unused")
	missing := filepath.Join(t.TempDir(), "kubelet")
	err := verifyInstalledExecutables(upgradedCatalogArtifact(olderSum, defaultSum), "", func(string) string { return missing })
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, FileNotFoundError), "an absent binary must read as missing, not unknown: %v", err)
}

func TestVerifyInstalledExecutables_NoChecksumsForPlatform(t *testing.T) {
	resolve, olderSum, defaultSum := installedBinary(t, "kubelet-1.33.4")
	artifact := upgradedCatalogArtifact(olderSum, defaultSum).withPlatform("linux", "riscv64")
	err := verifyInstalledExecutables(artifact, "", resolve)
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, PlatformNotFoundError), "expected PlatformNotFoundError, got %v", err)
}

func TestIdentifyArtifactVersions(t *testing.T) {
	resolve, olderSum, defaultSum := installedBinary(t, "kubelet-1.33.4")
	got, err := identifyArtifactVersions(upgradedCatalogArtifact(olderSum, defaultSum), resolve)
	require.NoError(t, err)
	assert.Equal(t, []string{olderVersion}, got)

	unknown, _, _ := installedBinary(t, "something-else")
	got, err = identifyArtifactVersions(upgradedCatalogArtifact(olderSum, defaultSum), unknown)
	require.NoError(t, err)
	assert.Empty(t, got, "present but unknown is an empty result, not an error")
}

func TestIdentifyArtifactVersions_VersionWithExtraBinary(t *testing.T) {
	// cri-o style: the newer release ships one more binary. A host on the older
	// release lacks that file; that rules out the newer version but must not
	// stop the older one from matching.
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte(content), 0o755))
		return p
	}
	write("crio", "crio-old")
	write("crun", "crun-old")
	_, crioOld := writeTempBinary(t, "crio-old")
	_, crunOld := writeTempBinary(t, "crun-old")
	_, crioNew := writeTempBinary(t, "crio-new")
	_, extraNew := writeTempBinary(t, "extra-new")

	artifact := (&ArtifactMetadata{
		Name: "cri-o",
		Versions: map[Version]VersionDetails{
			olderVersion: {Binaries: []BinaryDetail{
				{Name: "crio", PlatformChecksum: linuxAmd64Checksum("sha256", crioOld)},
				{Name: "crun", PlatformChecksum: linuxAmd64Checksum("sha256", crunOld)},
			}},
			defaultVersion: {Binaries: []BinaryDetail{
				{Name: "crio", PlatformChecksum: linuxAmd64Checksum("sha256", crioNew)},
				{Name: "crun", PlatformChecksum: linuxAmd64Checksum("sha256", crunOld)},
				{Name: "extra", PlatformChecksum: linuxAmd64Checksum("sha256", extraNew)},
			}},
		},
	}).withPlatform("linux", "amd64")

	got, err := identifyArtifactVersions(artifact, func(name string) string { return filepath.Join(dir, name) })
	require.NoError(t, err)
	assert.Equal(t, []string{olderVersion}, got)
}

func TestVerifyInstalledExecutables_UnopenableBinaryIsNotReportedMissing(t *testing.T) {
	// A symlink loop exists but cannot be opened (ELOOP). Reporting it as
	// missing would let kube cluster uninstall skip the reset as if kubeadm
	// were absent.
	dir := t.TempDir()
	loop := filepath.Join(dir, "a")
	require.NoError(t, os.Symlink(filepath.Join(dir, "b"), loop))
	require.NoError(t, os.Symlink(loop, filepath.Join(dir, "b")))

	_, olderSum, defaultSum := installedBinary(t, "unused")
	for name, recorded := range map[string]string{"recorded version": olderVersion, "identification": ""} {
		t.Run(name, func(t *testing.T) {
			err := verifyInstalledExecutables(upgradedCatalogArtifact(olderSum, defaultSum), recorded, func(string) string { return loop })
			require.Error(t, err)
			assert.False(t, errorx.IsOfType(err, FileNotFoundError), "an existing but unopenable binary must not read as absent: %v", err)
			assert.True(t, errorx.IsOfType(err, FileSystemError), "expected FileSystemError, got %v", err)
		})
	}
}
