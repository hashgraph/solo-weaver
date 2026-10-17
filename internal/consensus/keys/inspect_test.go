// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func keyByName(r *InspectReport, name string) KeyInspection {
	for _, k := range r.Keys {
		if k.Key == name {
			return k
		}
	}
	return KeyInspection{Key: name, Error: "missing"}
}

func TestInspectDir_Valid(t *testing.T) {
	dir := writeGeneratedFolder(t)

	r, err := InspectDir(dir, Selectors{})
	require.NoError(t, err)
	assert.False(t, r.HasErrors(), "%+v", r.Keys)

	gossip := keyByName(r, "gossip")
	assert.Equal(t, FileGossipCert, gossip.File)
	assert.Equal(t, "gossip certificate", gossip.Kind)
	require.NotNil(t, gossip.Certificate)
	assert.Equal(t, "RSA", gossip.Certificate.PublicKeyAlgorithm)
	assert.Equal(t, 3072, gossip.Certificate.PublicKeyBits)
	assert.True(t, gossip.Certificate.SelfSigned)
	assert.Positive(t, gossip.Certificate.DaysRemaining)
	assert.Len(t, gossip.Certificate.SHA384, 96)

	grpc := keyByName(r, "grpc")
	require.NotNil(t, grpc.Certificate)
	assert.Contains(t, grpc.Certificate.DNSNames, "localhost")

	admin := keyByName(r, "admin")
	require.NotNil(t, admin.AdminPublic)
	assert.Equal(t, "ed25519", admin.AdminPublic.Algorithm)
}

func TestInspectDir_NeverExposesPrivateKeyBytes(t *testing.T) {
	dir := writeGeneratedFolder(t)
	r, err := InspectDir(dir, Selectors{})
	require.NoError(t, err)

	out, err := json.Marshal(r)
	require.NoError(t, err)
	priv, err := os.ReadFile(filepath.Join(dir, FileAdminPrivate))
	require.NoError(t, err)
	assert.NotContains(t, string(out), "PRIVATE KEY")
	assert.NotContains(t, string(out), string(priv))
}

func TestInspectDir_DoesNotNeedPrivateKeys(t *testing.T) {
	dir := writeGeneratedFolder(t)
	for _, f := range []string{FileGossipPrivate, FileGrpcKey, FileAdminPrivate} {
		require.NoError(t, os.Remove(filepath.Join(dir, f)))
	}
	r, err := InspectDir(dir, Selectors{})
	require.NoError(t, err)
	assert.False(t, r.HasErrors(), "%+v", r.Keys)
}

func TestInspectDir_MissingIsSkippedCorruptIsError(t *testing.T) {
	dir := t.TempDir()
	r, err := InspectDir(dir, Selectors{Grpc: true})
	require.NoError(t, err)
	assert.False(t, r.HasErrors())
	assert.Empty(t, r.Keys)
	assert.Equal(t, []string{FileGrpcCert}, r.Missing)

	require.NoError(t, os.WriteFile(filepath.Join(dir, FileGrpcCert), []byte("junk"), 0o644))
	r, err = InspectDir(dir, Selectors{Grpc: true})
	require.NoError(t, err)
	assert.True(t, r.HasErrors())
	assert.Contains(t, keyByName(r, "grpc").Error, "does not parse")
}

func TestInspectDir_PartialFolder(t *testing.T) {
	dir := writeGeneratedFolder(t)
	require.NoError(t, os.Remove(filepath.Join(dir, FileGossipCert)))
	require.NoError(t, os.Remove(filepath.Join(dir, FileAdminPublic)))

	r, err := InspectDir(dir, Selectors{})
	require.NoError(t, err)
	assert.False(t, r.HasErrors())
	require.Len(t, r.Keys, 1)
	assert.Equal(t, FileGrpcCert, r.Keys[0].File)
	assert.ElementsMatch(t, []string{FileGossipCert, FileAdminPublic}, r.Missing)
}

func TestInspectFile(t *testing.T) {
	dir := writeGeneratedFolder(t)

	r, err := InspectFile(filepath.Join(dir, FileGrpcCert))
	require.NoError(t, err)
	assert.Equal(t, FileGrpcCert, r.Keys[0].File)
	assert.Equal(t, "gRPC TLS certificate", r.Keys[0].Kind)
	require.NotNil(t, r.Keys[0].Certificate)

	r, err = InspectFile(filepath.Join(dir, FileAdminPublic))
	require.NoError(t, err)
	require.NotNil(t, r.Keys[0].AdminPublic)

	r, err = InspectFile(filepath.Join(dir, FileGrpcKey))
	require.NoError(t, err)
	assert.NotEmpty(t, r.Keys[0].Error, "a private key file is not inspectable")

	_, err = InspectFile(filepath.Join(dir, "nope"))
	require.Error(t, err)
}
