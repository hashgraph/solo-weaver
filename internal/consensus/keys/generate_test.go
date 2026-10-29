// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	corekeys "github.com/hashgraph/solo-weaver/pkg/consensus/keys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeSelectors(t *testing.T) {
	// Empty -> all.
	assert.Equal(t, Selectors{Gossip: true, Grpc: true, Admin: true}, NormalizeSelectors(Selectors{}))
	// Any set -> unchanged.
	assert.Equal(t, Selectors{Grpc: true}, NormalizeSelectors(Selectors{Grpc: true}))
}

func TestGenerate_AllTypes(t *testing.T) {
	set, err := Generate(GenerateOptions{MachineID: "m-uuid", Identity: "op-a"})
	require.NoError(t, err)
	require.NotNil(t, set.Gossip)
	require.NotNil(t, set.Grpc)
	assert.Equal(t, "m-uuid", set.Gossip.Certificate.Subject.CommonName)
	assert.Equal(t, "localhost", set.Grpc.Certificate.Subject.CommonName)
}

func TestGenerate_SelectorSubset(t *testing.T) {
	set, err := Generate(GenerateOptions{
		Selectors: Selectors{Gossip: true},
		MachineID: "m",
	})
	require.NoError(t, err)
	assert.NotNil(t, set.Gossip)
	assert.Nil(t, set.Grpc)
}

func TestWriteToDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	set, err := Generate(GenerateOptions{MachineID: "m", Identity: "op"})
	require.NoError(t, err)

	require.NoError(t, set.WriteToDir(dir))

	// All neutral-named files exist; no combined artifacts file is written.
	for _, name := range []string{
		FileGossipPrivate, FileGossipCert, FileGrpcKey, FileGrpcCert,
	} {
		assert.FileExists(t, filepath.Join(dir, name), "expected %s", name)
	}
	assert.NoFileExists(t, filepath.Join(dir, "nodeCreate.json"))

	// Private key material is 0600; public files are 0644 (skip perm assert on
	// non-unix where mode bits differ).
	if runtime.GOOS != "windows" {
		assertPerm(t, filepath.Join(dir, FileGossipPrivate), 0o600)
		assertPerm(t, filepath.Join(dir, FileGrpcKey), 0o600)
		assertPerm(t, filepath.Join(dir, FileGossipCert), 0o644)
	}
}

func TestWriteToDir_TightensExistingPerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits differ on windows")
	}
	dir := filepath.Join(t.TempDir(), "keys")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	stale := filepath.Join(dir, FileGossipPrivate)
	require.NoError(t, os.WriteFile(stale, []byte("old"), 0o644))
	require.NoError(t, os.Chmod(stale, 0o644))

	set, err := Generate(GenerateOptions{MachineID: "m"})
	require.NoError(t, err)
	require.NoError(t, set.WriteToDir(dir))

	assertPerm(t, stale, 0o600)
	got, err := os.ReadFile(stale)
	require.NoError(t, err)
	assert.NotEqual(t, []byte("old"), got)

	// No temp files remain after the rename.
	tmps, err := filepath.Glob(filepath.Join(dir, ".*.tmp.*"))
	require.NoError(t, err)
	assert.Empty(t, tmps)
}

func TestWriteToDir_SubsetOmitsFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gossip-only")
	set, err := Generate(GenerateOptions{Selectors: Selectors{Gossip: true}, MachineID: "m"})
	require.NoError(t, err)

	require.NoError(t, set.WriteToDir(dir))

	assert.FileExists(t, filepath.Join(dir, FileGossipPrivate))
	assert.NoFileExists(t, filepath.Join(dir, FileGrpcKey))
}

func TestSelectorsHelpers(t *testing.T) {
	assert.True(t, Selectors{Grpc: true}.Any())
	assert.False(t, Selectors{}.Any())
	assert.Equal(t, Selectors{Gossip: true},
		Selectors{Gossip: true, Grpc: true}.Minus(Selectors{Grpc: true}))
}

func TestExistingSelectors(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	set, err := Generate(GenerateOptions{MachineID: "m"})
	require.NoError(t, err)
	require.NoError(t, set.WriteToDir(dir))
	assert.Equal(t, Selectors{Gossip: true, Grpc: true}, ExistingSelectors(dir))

	empty := t.TempDir()
	assert.Equal(t, Selectors{}, ExistingSelectors(empty))
}

func assertPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, want, fi.Mode().Perm(), "perm on %s", path)
}

func TestGenerate_GrpcOptions(t *testing.T) {
	set, err := Generate(GenerateOptions{
		Selectors: Selectors{Grpc: true},
		Grpc:      corekeys.GrpcOptions{DNSNames: []string{"node0.example.org"}, ValidityDays: 30},
	})
	require.NoError(t, err)
	assert.Contains(t, set.Grpc.Certificate.DNSNames, "node0.example.org")
	assert.Contains(t, set.Grpc.Certificate.DNSNames, "localhost")
	assert.Less(t, set.Grpc.Certificate.NotAfter.Sub(set.Grpc.Certificate.NotBefore), 32*24*time.Hour)

	_, err = Generate(GenerateOptions{Selectors: Selectors{Grpc: true}, Grpc: corekeys.GrpcOptions{ValidityDays: -5}})
	require.Error(t, err)
}
