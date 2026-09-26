// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func findingFor(r *VerifyReport, key string) Finding {
	for _, f := range r.Findings {
		if f.Key == key {
			return f
		}
	}
	return Finding{Key: key, Detail: "missing finding"}
}

func TestVerifyDir_Valid(t *testing.T) {
	dir := writeGeneratedFolder(t) // full set, ed25519 admin

	report, err := VerifyDir(dir, Selectors{})
	require.NoError(t, err)
	assert.True(t, report.OK(), "expected all findings OK: %+v", report.Findings)

	gossip := findingFor(report, "gossip")
	assert.True(t, gossip.OK)
	assert.Contains(t, gossip.Detail, "RSA-3072")

	assert.Equal(t, "ed25519", findingFor(report, "admin").Detail)
}

func TestVerifyDir_ECDSAAdmin(t *testing.T) {
	dir := writeGeneratedFolder(t)
	// A compressed secp256k1 public key (33 bytes, 0x02 prefix) from a wallet.
	pub := "02" + strings.Repeat("ab", 32)
	require.NoError(t, os.WriteFile(filepath.Join(dir, FileAdminPublic), []byte(pub+"\n"), 0o644))

	report, err := VerifyDir(dir, Selectors{Admin: true})
	require.NoError(t, err)
	assert.True(t, report.OK())
	assert.Contains(t, findingFor(report, "admin").Detail, "ecdsa")
}

func TestVerifyDir_NoAdminPubIsNotAFailureByDefault(t *testing.T) {
	dir := writeGeneratedFolder(t)
	require.NoError(t, os.Remove(filepath.Join(dir, FileAdminPublic)))

	report, err := VerifyDir(dir, Selectors{})
	require.NoError(t, err)
	assert.True(t, report.OK(), "%+v", report.Findings)

	report, err = VerifyDir(dir, Selectors{Admin: true})
	require.NoError(t, err)
	assert.False(t, report.OK())
}

func TestVerifyDir_DetectsTamperedCert(t *testing.T) {
	dir := writeGeneratedFolder(t)
	// Overwrite the gossip cert with a different key's cert -> mismatch.
	other, err := Generate(GenerateOptions{Selectors: Selectors{Gossip: true}, MachineID: "other"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, FileGossipCert), other.Gossip.CertificatePEM(), 0o644))

	report, err := VerifyDir(dir, Selectors{Gossip: true})
	require.NoError(t, err)
	assert.False(t, report.OK())
	assert.Contains(t, findingFor(report, "gossip").Detail, "does not match")
}

func TestVerifyDir_MissingFile(t *testing.T) {
	report, err := VerifyDir(t.TempDir(), Selectors{Grpc: true})
	require.NoError(t, err)
	assert.False(t, report.OK())
	assert.Contains(t, findingFor(report, "grpc").Detail, "missing")
}

func TestVerifyDir_CorruptPEM(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "k")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, FileGrpcKey), []byte("not pem"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, FileGrpcCert), []byte("not pem"), 0o644))

	report, err := VerifyDir(dir, Selectors{Grpc: true})
	require.NoError(t, err)
	assert.False(t, report.OK())
	assert.Contains(t, findingFor(report, "grpc").Detail, "does not parse")
}

func TestVerifyCluster_Valid(t *testing.T) {
	dir := writeGeneratedFolder(t)
	client := newFakeSecretClient()
	_, err := Import(context.Background(), client, ImportOptions{Namespace: "ns", NodeId: 0, FromDir: dir})
	require.NoError(t, err)

	report, err := VerifyCluster(context.Background(), client, "ns", 0, Selectors{}, ResolveOptions{})
	require.NoError(t, err)
	assert.True(t, report.OK(), "%+v", report.Findings)
	assert.Equal(t, "ed25519", findingFor(report, "admin").Detail)
}

func TestVerifyCluster_MissingSecret(t *testing.T) {
	client := newFakeSecretClient()
	report, err := VerifyCluster(context.Background(), client, "ns", 0, Selectors{Gossip: true}, ResolveOptions{})
	require.NoError(t, err)
	assert.False(t, report.OK())
	assert.Contains(t, findingFor(report, "gossip").Detail, "no gossip key secret found")
}

func TestVerifyCluster_AdminSecretMustBeRawBytes(t *testing.T) {
	dir := writeGeneratedFolder(t)
	client := newFakeSecretClient()
	_, err := Import(context.Background(), client, ImportOptions{Namespace: "ns", NodeId: 0, FromDir: dir})
	require.NoError(t, err)

	ok, err := VerifyCluster(context.Background(), client, "ns", 0, Selectors{Admin: true}, ResolveOptions{})
	require.NoError(t, err)
	assert.True(t, ok.OK(), "%+v", ok.Findings)

	// A secret holding the hex text (double-encoded by the operator) is flagged.
	hexText, err := os.ReadFile(filepath.Join(dir, FileAdminPublic))
	require.NoError(t, err)
	name := client.onlySecret(t, "ns", "node0-admin-pubkey-")
	client.store["ns/"+name] = map[string][]byte{FileAdminPublic: []byte(strings.TrimSpace(string(hexText)))}
	bad, err := VerifyCluster(context.Background(), client, "ns", 0, Selectors{Admin: true}, ResolveOptions{})
	require.NoError(t, err)
	assert.False(t, bad.OK())
	assert.Contains(t, findingFor(bad, "admin").Detail, "not raw key bytes")
}

func TestVerifyCluster_SeveralGenerationsListedUnlessChosen(t *testing.T) {
	client, names := importTwoGenerations(t)

	report, err := VerifyCluster(context.Background(), client, "ns", 0, Selectors{Gossip: true}, ResolveOptions{})
	require.NoError(t, err)
	assert.False(t, report.OK())
	assert.Contains(t, findingFor(report, "gossip").Detail, names[0])
	assert.Contains(t, findingFor(report, "gossip").Detail, names[1])

	fp := client.labels["ns/"+names[0]][LabelFingerprint]
	report, err = VerifyCluster(context.Background(), client, "ns", 0, Selectors{Gossip: true}, ResolveOptions{Fingerprint: fp[:8]})
	require.NoError(t, err)
	assert.True(t, report.OK(), "%+v", report.Findings)
}
