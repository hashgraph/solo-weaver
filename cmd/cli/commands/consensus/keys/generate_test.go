// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	cnkeys "github.com/hashgraph/solo-weaver/internal/consensus/keys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runGenerateCmd executes `keys generate` with args against a fresh command tree
// and returns combined output. Flags are package-level vars, so reset them first.
func runGenerateCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	flagKeysDir, flagIdentity, flagMachineID = "", "", ""
	flagGossip, flagTLS, flagAdmin = false, false, false
	flagAdminAlgo = "ed25519"
	flagGenerateForce = false

	root := GetCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(append([]string{"generate"}, args...))
	err := root.Execute()
	return buf.String(), err
}

func TestGenerateCmd_NodeIdFree(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out")
	out, err := runGenerateCmd(t, "--keys-dir", dir, "--machine-id", "m-uuid")
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(dir, cnkeys.FileGossipPrivate))
	assert.FileExists(t, filepath.Join(dir, cnkeys.FileGrpcCert))
	assert.FileExists(t, filepath.Join(dir, cnkeys.FileAdminPublic))
	assert.Contains(t, out, "Generated key material")
	assert.Contains(t, out, "gRPC cert hash (SHA-384):")
}

func TestGenerateCmd_RequiresKeysDir(t *testing.T) {
	_, err := runGenerateCmd(t)
	require.Error(t, err)
}

func TestGenerateCmd_RejectsBadAdminAlgo(t *testing.T) {
	dir := t.TempDir()
	_, err := runGenerateCmd(t, "--keys-dir", dir, "--machine-id", "m", "--admin-algo", "rsa")
	require.Error(t, err)
}

func TestGenerateCmd_SelectorSubset(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out")
	_, err := runGenerateCmd(t, "--keys-dir", dir, "--machine-id", "m", "--gossip")
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(dir, cnkeys.FileGossipPrivate))
	assert.NoFileExists(t, filepath.Join(dir, cnkeys.FileGrpcKey))
	assert.NoFileExists(t, filepath.Join(dir, cnkeys.FileAdminPrivate))
}

func TestGenerateCmd_Idempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	_, err := runGenerateCmd(t, "--keys-dir", dir, "--machine-id", "m", "--gossip")
	require.NoError(t, err)
	first, err := os.ReadFile(filepath.Join(dir, cnkeys.FileGossipPrivate))
	require.NoError(t, err)

	// Re-running does not regenerate: the existing key is kept.
	out, err := runGenerateCmd(t, "--keys-dir", dir, "--machine-id", "m", "--gossip")
	require.NoError(t, err)
	assert.Contains(t, out, "already exists")
	again, err := os.ReadFile(filepath.Join(dir, cnkeys.FileGossipPrivate))
	require.NoError(t, err)
	assert.Equal(t, string(first), string(again), "existing key must be preserved")

	// --force regenerates (overwrites).
	_, err = runGenerateCmd(t, "--keys-dir", dir, "--machine-id", "m", "--gossip", "--force")
	require.NoError(t, err)
	forced, err := os.ReadFile(filepath.Join(dir, cnkeys.FileGossipPrivate))
	require.NoError(t, err)
	assert.NotEqual(t, string(first), string(forced), "--force must regenerate")
}

func TestGenerateCmd_DistinctPerRun(t *testing.T) {
	dirA := filepath.Join(t.TempDir(), "a")
	dirB := filepath.Join(t.TempDir(), "b")
	_, err := runGenerateCmd(t, "--keys-dir", dirA, "--machine-id", "m", "--gossip")
	require.NoError(t, err)
	_, err = runGenerateCmd(t, "--keys-dir", dirB, "--machine-id", "m", "--gossip")
	require.NoError(t, err)

	a, err := os.ReadFile(filepath.Join(dirA, cnkeys.FileGossipPrivate))
	require.NoError(t, err)
	b, err := os.ReadFile(filepath.Join(dirB, cnkeys.FileGossipPrivate))
	require.NoError(t, err)
	assert.NotEqual(t, string(a), string(b))
}
