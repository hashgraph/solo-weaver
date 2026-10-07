// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	corekeys "github.com/hashgraph/solo-weaver/pkg/consensus/keys"
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

	// admin folder verification also confirms pub<->priv correspondence.
	assert.Contains(t, findingFor(report, "admin").Detail, "matches private key")
}

func TestVerifyDir_ECDSAAdmin(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "k")
	set, err := Generate(GenerateOptions{MachineID: "m", AdminAlgo: corekeys.AdminECDSA})
	require.NoError(t, err)
	require.NoError(t, set.WriteToDir(dir))

	report, err := VerifyDir(dir, Selectors{Admin: true})
	require.NoError(t, err)
	assert.True(t, report.OK())
	assert.Contains(t, findingFor(report, "admin").Detail, "ecdsa")
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

	report, err := VerifyCluster(context.Background(), client, "ns", 0, Selectors{})
	require.NoError(t, err)
	assert.True(t, report.OK(), "%+v", report.Findings)
	// Cluster admin secret has no private key, so no correspondence claim.
	assert.Equal(t, "ed25519", findingFor(report, "admin").Detail)
}

func TestVerifyCluster_MissingSecret(t *testing.T) {
	client := newFakeSecretClient()
	report, err := VerifyCluster(context.Background(), client, "ns", 0, Selectors{Gossip: true})
	require.NoError(t, err)
	assert.False(t, report.OK())
	assert.Contains(t, findingFor(report, "gossip").Detail, "missing")
}
