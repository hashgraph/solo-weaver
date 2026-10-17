// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cnkeys "github.com/hashgraph/solo-weaver/internal/consensus/keys"
	corekeys "github.com/hashgraph/solo-weaver/pkg/consensus/keys"
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
	assert.True(t, fake.hasSecretWithPrefix("orbit-1", "node0-gossip-keys-"))
	assert.True(t, fake.hasSecretWithPrefix("orbit-1", "node0-grpc-tls-keys-"))
	assert.True(t, fake.hasSecretWithPrefix("orbit-1", "node0-admin-pubkey-"))

	// Admin secret carries only the public key.
	for k, admin := range fake.store {
		if strings.HasPrefix(k, "orbit-1/node0-admin-pubkey-") {
			assert.Contains(t, admin, cnkeys.FileAdminPublic)
			assert.NotContains(t, admin, cnkeys.FileAdminPrivate)
		}
	}

	// The output names the exact secrets, so they can be passed to install.
	assert.Contains(t, out, "secret/node0-admin-pubkey-")
	assert.Contains(t, out, "secret/node0-gossip-keys-")
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

	assert.True(t, fake.hasSecretWithPrefix("ns", "node1-gossip-keys-"))
	assert.False(t, fake.hasSecretWithPrefix("ns", "node1-grpc-tls-keys-"))
	assert.False(t, fake.hasSecretWithPrefix("ns", "node1-admin-pubkey-"))
}

func TestInitCmd_GrpcFlags(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	stubSecretClient(t, &fakeSecretClient{store: map[string]map[string][]byte{}})
	resetGrpcFlags(t, initCmd)
	t.Cleanup(func() { resetGrpcFlags(t, initCmd) })

	_, err := runUnderNode(t, initCmd, "--namespace", "ns", "--node-id", "0", "--keys-dir", dir,
		"--grpc-dns", "node0.example.org", "--grpc-cn", "node0", "--grpc-org", "Example Org", "--grpc-validity-days", "90")
	require.NoError(t, err)

	pemBytes, err := os.ReadFile(filepath.Join(dir, cnkeys.FileGrpcCert))
	require.NoError(t, err)
	cert, err := corekeys.ParseCertificatePEM(pemBytes)
	require.NoError(t, err)
	assert.Equal(t, "node0", cert.Subject.CommonName)
	assert.Equal(t, []string{"Example Org"}, cert.Subject.Organization)
	assert.Contains(t, cert.DNSNames, "node0.example.org")
	assert.Less(t, cert.NotAfter.Sub(cert.NotBefore), 92*24*time.Hour)
}
