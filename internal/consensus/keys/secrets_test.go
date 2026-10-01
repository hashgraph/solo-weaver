// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSecretClient is an in-memory SecretClient for tests.
type fakeSecretClient struct {
	store map[string]map[string][]byte // key: namespace/name
}

func newFakeSecretClient() *fakeSecretClient {
	return &fakeSecretClient{store: map[string]map[string][]byte{}}
}

func (f *fakeSecretClient) key(ns, name string) string { return ns + "/" + name }

func (f *fakeSecretClient) ApplySecret(_ context.Context, ns, name string, data map[string][]byte, _ map[string]string) error {
	cp := make(map[string][]byte, len(data))
	for k, v := range data {
		cp[k] = append([]byte(nil), v...)
	}
	f.store[f.key(ns, name)] = cp
	return nil
}

func (f *fakeSecretClient) GetSecretData(_ context.Context, ns, name string) (map[string][]byte, bool, error) {
	d, ok := f.store[f.key(ns, name)]
	return d, ok, nil
}

func TestSecretNamesFor(t *testing.T) {
	n := SecretNamesFor(3)
	assert.Equal(t, "node3-gossip-keys", n.Gossip)
	assert.Equal(t, "node3-grpc-tls-keys", n.Grpc)
	assert.Equal(t, "node3-admin-pubkey", n.Admin)
}

// writeGeneratedFolder generates a full key set and writes it to a temp dir.
func writeGeneratedFolder(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "keys")
	set, err := Generate(GenerateOptions{MachineID: "m"})
	require.NoError(t, err)
	require.NoError(t, set.WriteToDir(dir))
	return dir
}

func TestImport_AllSecrets(t *testing.T) {
	dir := writeGeneratedFolder(t)
	client := newFakeSecretClient()

	res, err := Import(context.Background(), client, ImportOptions{
		Namespace: "orbit-1",
		NodeId:    0,
		FromDir:   dir,
	})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"node0-gossip-keys", "node0-grpc-tls-keys", "node0-admin-pubkey"}, res.Applied)

	// Gossip secret carries both signing files.
	gossip, ok, _ := client.GetSecretData(context.Background(), "orbit-1", "node0-gossip-keys")
	require.True(t, ok)
	assert.Contains(t, gossip, FileGossipPrivate)
	assert.Contains(t, gossip, FileGossipCert)

	// Admin secret carries ONLY the public key — never admin.key.
	admin, ok, _ := client.GetSecretData(context.Background(), "orbit-1", "node0-admin-pubkey")
	require.True(t, ok)
	assert.Contains(t, admin, FileAdminPublic)
	assert.NotContains(t, admin, FileAdminPrivate)
}

func TestImport_GossipGuard(t *testing.T) {
	dir := writeGeneratedFolder(t)
	client := newFakeSecretClient()
	opts := ImportOptions{Namespace: "ns", NodeId: 1, FromDir: dir, Selectors: Selectors{Gossip: true}}

	// First import succeeds.
	_, err := Import(context.Background(), client, opts)
	require.NoError(t, err)

	// Re-importing the SAME material is idempotent (no error).
	_, err = Import(context.Background(), client, opts)
	require.NoError(t, err)

	// Importing DIFFERENT gossip material to the same node is refused...
	other := writeGeneratedFolder(t)
	diff := ImportOptions{Namespace: "ns", NodeId: 1, FromDir: other, Selectors: Selectors{Gossip: true}}
	_, err = Import(context.Background(), client, diff)
	require.Error(t, err)

	// ...unless forced.
	diff.Force = true
	_, err = Import(context.Background(), client, diff)
	require.NoError(t, err)
}

func TestImport_MissingFile(t *testing.T) {
	client := newFakeSecretClient()
	_, err := Import(context.Background(), client, ImportOptions{
		Namespace: "ns", NodeId: 0, FromDir: t.TempDir(), Selectors: Selectors{Gossip: true},
	})
	require.Error(t, err)
}

func TestExport_RoundTrip(t *testing.T) {
	dir := writeGeneratedFolder(t)
	client := newFakeSecretClient()
	_, err := Import(context.Background(), client, ImportOptions{Namespace: "ns", NodeId: 2, FromDir: dir})
	require.NoError(t, err)

	out := filepath.Join(t.TempDir(), "export")
	res, err := Export(context.Background(), client, ExportOptions{Namespace: "ns", NodeId: 2, OutDir: out})
	require.NoError(t, err)
	assert.Len(t, res.Exported, 3)

	// Files land with the neutral names; gossip private key is 0600.
	assert.FileExists(t, filepath.Join(out, FileGossipPrivate))
	assert.FileExists(t, filepath.Join(out, FileGrpcCert))
	assert.FileExists(t, filepath.Join(out, FileAdminPublic))
	// admin.key is never in the cluster, so it is not exported.
	assert.NoFileExists(t, filepath.Join(out, FileAdminPrivate))

	fi, err := os.Stat(filepath.Join(out, FileGossipPrivate))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
}

func TestExport_Idempotent(t *testing.T) {
	dir := writeGeneratedFolder(t)
	client := newFakeSecretClient()
	_, err := Import(context.Background(), client, ImportOptions{Namespace: "ns", NodeId: 0, FromDir: dir})
	require.NoError(t, err)

	out := filepath.Join(t.TempDir(), "export")
	first, err := Export(context.Background(), client, ExportOptions{Namespace: "ns", NodeId: 0, OutDir: out})
	require.NoError(t, err)
	assert.Len(t, first.Exported, 3)

	// Re-export is a no-op: nothing written, all skipped.
	second, err := Export(context.Background(), client, ExportOptions{Namespace: "ns", NodeId: 0, OutDir: out})
	require.NoError(t, err)
	assert.Empty(t, second.Exported)
	assert.Len(t, second.Skipped, 3)

	// A file with different content is refused without force...
	require.NoError(t, os.WriteFile(filepath.Join(out, FileAdminPublic), []byte("deadbeef\n"), 0o644))
	_, err = Export(context.Background(), client, ExportOptions{Namespace: "ns", NodeId: 0, OutDir: out, Selectors: Selectors{Admin: true}})
	require.Error(t, err)

	// ...and overwritten with force.
	_, err = Export(context.Background(), client, ExportOptions{Namespace: "ns", NodeId: 0, OutDir: out, Selectors: Selectors{Admin: true}, Force: true})
	require.NoError(t, err)
}

func TestExport_MissingSecret(t *testing.T) {
	client := newFakeSecretClient()
	_, err := Export(context.Background(), client, ExportOptions{
		Namespace: "ns", NodeId: 0, OutDir: t.TempDir(), Selectors: Selectors{Admin: true},
	})
	require.Error(t, err)
}
