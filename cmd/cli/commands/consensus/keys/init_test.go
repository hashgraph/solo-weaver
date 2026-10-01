// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"os"
	"path/filepath"
	"testing"

	cnkeys "github.com/hashgraph/solo-weaver/internal/consensus/keys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitCmd_HappyPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	fake := &fakeSecretClient{store: map[string]map[string][]byte{}}
	stubSecretClient(t, fake)

	out, err := runUnderNode(t, initCmd, "--namespace", "orbit-1", "--node-id", "0", "--keys-dir", dir)
	require.NoError(t, err)

	// Generated the folder AND created the three secrets in one invocation.
	assert.FileExists(t, filepath.Join(dir, cnkeys.FileGossipPrivate))
	assert.FileExists(t, filepath.Join(dir, cnkeys.FileAdminPrivate))
	assert.Contains(t, fake.store, "orbit-1/node0-gossip-keys")
	assert.Contains(t, fake.store, "orbit-1/node0-grpc-tls-keys")
	assert.Contains(t, fake.store, "orbit-1/node0-admin-pubkey")

	// Admin secret carries only the public key.
	admin := fake.store["orbit-1/node0-admin-pubkey"]
	assert.Contains(t, admin, cnkeys.FileAdminPublic)
	assert.NotContains(t, admin, cnkeys.FileAdminPrivate)

	assert.Contains(t, out, "secret/node0-admin-pubkey")
}

func TestInitCmd_RequiresKeysDir(t *testing.T) {
	stubSecretClient(t, &fakeSecretClient{store: map[string]map[string][]byte{}})
	_, err := runUnderNode(t, initCmd, "--namespace", "ns", "--node-id", "0")
	require.Error(t, err)
}

func TestInitCmd_RequiresNodeId(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	stubSecretClient(t, &fakeSecretClient{store: map[string]map[string][]byte{}})
	_, err := runUnderNode(t, initCmd, "--namespace", "ns", "--keys-dir", dir)
	require.Error(t, err)
}

func TestInitCmd_Idempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	fake := &fakeSecretClient{store: map[string]map[string][]byte{}}
	stubSecretClient(t, fake)

	_, err := runUnderNode(t, initCmd, "--namespace", "ns", "--node-id", "0", "--keys-dir", dir)
	require.NoError(t, err)
	first, err := os.ReadFile(filepath.Join(dir, cnkeys.FileGossipPrivate))
	require.NoError(t, err)

	// Re-running init keeps the existing key material (no regeneration) and does
	// not fail on the already-present gossip secret.
	_, err = runUnderNode(t, initCmd, "--namespace", "ns", "--node-id", "0", "--keys-dir", dir)
	require.NoError(t, err)
	again, err := os.ReadFile(filepath.Join(dir, cnkeys.FileGossipPrivate))
	require.NoError(t, err)
	assert.Equal(t, string(first), string(again))
}

func TestInitCmd_EquivalentToGenerateThenImport(t *testing.T) {
	// init with a selector subset generates + imports only that type.
	dir := filepath.Join(t.TempDir(), "keys")
	fake := &fakeSecretClient{store: map[string]map[string][]byte{}}
	stubSecretClient(t, fake)

	_, err := runUnderNode(t, initCmd, "--namespace", "ns", "--node-id", "1", "--keys-dir", dir, "--gossip")
	require.NoError(t, err)

	assert.Contains(t, fake.store, "ns/node1-gossip-keys")
	assert.NotContains(t, fake.store, "ns/node1-grpc-tls-keys")
	assert.NotContains(t, fake.store, "ns/node1-admin-pubkey")
}
