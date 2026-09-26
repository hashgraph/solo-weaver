// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

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
	require.NotNil(t, set.Admin)
	assert.Equal(t, "m-uuid", set.Gossip.Certificate.Subject.CommonName)
	assert.Equal(t, "localhost", set.Grpc.Certificate.Subject.CommonName)
	assert.Equal(t, corekeys.AdminEd25519, set.Admin.Algo) // default algo
}

func TestGenerate_SelectorSubset(t *testing.T) {
	set, err := Generate(GenerateOptions{
		Selectors: Selectors{Gossip: true},
		MachineID: "m",
	})
	require.NoError(t, err)
	assert.NotNil(t, set.Gossip)
	assert.Nil(t, set.Grpc)
	assert.Nil(t, set.Admin)
}

func TestGenerate_AdminECDSA(t *testing.T) {
	set, err := Generate(GenerateOptions{
		Selectors: Selectors{Admin: true},
		AdminAlgo: corekeys.AdminECDSA,
	})
	require.NoError(t, err)
	require.NotNil(t, set.Admin)
	assert.Equal(t, corekeys.AdminECDSA, set.Admin.Algo)
}

func TestWriteToDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	set, err := Generate(GenerateOptions{MachineID: "m", Identity: "op"})
	require.NoError(t, err)

	require.NoError(t, set.WriteToDir(dir))

	// All neutral-named files exist; no combined artifacts file is written.
	for _, name := range []string{
		FileGossipPrivate, FileGossipCert, FileGrpcKey, FileGrpcCert,
		FileAdminPrivate, FileAdminPublic,
	} {
		assert.FileExists(t, filepath.Join(dir, name), "expected %s", name)
	}
	assert.NoFileExists(t, filepath.Join(dir, "nodeCreate.json"))

	// Private key material is 0600; public files are 0644 (skip perm assert on
	// non-unix where mode bits differ).
	if runtime.GOOS != "windows" {
		assertPerm(t, filepath.Join(dir, FileGossipPrivate), 0o600)
		assertPerm(t, filepath.Join(dir, FileGrpcKey), 0o600)
		assertPerm(t, filepath.Join(dir, FileAdminPrivate), 0o600)
		assertPerm(t, filepath.Join(dir, FileGossipCert), 0o644)
		assertPerm(t, filepath.Join(dir, FileAdminPublic), 0o644)
	}

	// The admin public file holds the hex public key, not private material.
	pub, err := os.ReadFile(filepath.Join(dir, FileAdminPublic))
	require.NoError(t, err)
	assert.Equal(t, set.Admin.PublicKeyHex()+"\n", string(pub))
}

func TestWriteToDir_SubsetOmitsFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gossip-only")
	set, err := Generate(GenerateOptions{Selectors: Selectors{Gossip: true}, MachineID: "m"})
	require.NoError(t, err)

	require.NoError(t, set.WriteToDir(dir))

	assert.FileExists(t, filepath.Join(dir, FileGossipPrivate))
	assert.NoFileExists(t, filepath.Join(dir, FileGrpcKey))
	assert.NoFileExists(t, filepath.Join(dir, FileAdminPrivate))
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
	assert.Equal(t, Selectors{Gossip: true, Grpc: true, Admin: true}, ExistingSelectors(dir))

	empty := t.TempDir()
	assert.Equal(t, Selectors{}, ExistingSelectors(empty))
}

func assertPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, want, fi.Mode().Perm(), "perm on %s", path)
}
