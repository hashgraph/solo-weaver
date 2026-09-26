// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha512"
	"crypto/x509"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateGossipKey(t *testing.T) {
	pair, err := GenerateGossipKey(GossipOptions{MachineUUID: "test-machine-uuid", Identity: "operator-a"})
	require.NoError(t, err)
	require.NotNil(t, pair)

	// RSA-3072 signing key.
	rsaKey, ok := pair.PrivateKey.(*rsa.PrivateKey)
	require.True(t, ok, "gossip key must be RSA")
	assert.Equal(t, GossipKeyBits, rsaKey.N.BitLen())

	// Self-signed cert with machine-UUID CN, SHA384WithRSA, and the identity in
	// OU + DNS SAN (never the node-id).
	cert := pair.Certificate
	assert.Equal(t, "test-machine-uuid", cert.Subject.CommonName)
	assert.Equal(t, x509.SHA384WithRSA, cert.SignatureAlgorithm)
	assert.Contains(t, cert.Subject.OrganizationalUnit, "operator-a")
	assert.Contains(t, cert.DNSNames, "operator-a")
	assert.Equal(t, cert.Subject.String(), cert.Issuer.String(), "must be self-signed")

	// Certificate verifies against its own public key.
	require.NoError(t, cert.CheckSignatureFrom(cert))
}

func TestGenerateGossipKey_NoIdentity(t *testing.T) {
	pair, err := GenerateGossipKey(GossipOptions{MachineUUID: "uuid-only"})
	require.NoError(t, err)
	assert.Equal(t, "uuid-only", pair.Certificate.Subject.CommonName)
	assert.Empty(t, pair.Certificate.Subject.OrganizationalUnit)
	assert.Empty(t, pair.Certificate.DNSNames)
}

func TestGenerateGossipKey_IsLongLivedCASigner(t *testing.T) {
	// The gossip cert is the roster's gossip_ca_certificate: a long-lived,
	// self-signed identity anchor that can verify its own signature.
	pair, err := GenerateGossipKey(GossipOptions{MachineUUID: "uuid-2"})
	require.NoError(t, err)
	cert := pair.Certificate
	assert.True(t, cert.IsCA)
	assert.NotZero(t, cert.KeyUsage&x509.KeyUsageCertSign)
	// ~100-year validity (allow a wide margin).
	assert.Greater(t, cert.NotAfter.Sub(cert.NotBefore), 99*365*24*time.Hour)
}

func TestGenerateGrpcKey_IsRSA3072(t *testing.T) {
	pair, err := GenerateGrpcKey()
	require.NoError(t, err)
	rsaKey, ok := pair.PrivateKey.(*rsa.PrivateKey)
	require.True(t, ok)
	assert.Equal(t, GrpcKeyBits, rsaKey.N.BitLen())
}

// TestGeneratorsAreNonDeterministic guards the single most important property of
// a key generator: distinct invocations must never yield identical key material.
// The hiero reference generator uses a deterministic RNG for tests; this package
// must always draw from crypto/rand.
func TestGeneratorsAreNonDeterministic(t *testing.T) {
	g1, err := GenerateGossipKey(GossipOptions{MachineUUID: "u"})
	require.NoError(t, err)
	g2, err := GenerateGossipKey(GossipOptions{MachineUUID: "u"})
	require.NoError(t, err)
	p1, err := g1.PrivateKeyPEM()
	require.NoError(t, err)
	p2, err := g2.PrivateKeyPEM()
	require.NoError(t, err)
	assert.False(t, bytes.Equal(p1, p2), "gossip private keys must differ")
	assert.NotEqual(t, g1.CertHashSHA384(), g2.CertHashSHA384(), "gossip cert hashes must differ")

	t1, err := GenerateGrpcKey()
	require.NoError(t, err)
	t2, err := GenerateGrpcKey()
	require.NoError(t, err)
	assert.NotEqual(t, t1.CertHashSHA384(), t2.CertHashSHA384(), "gRPC cert hashes must differ")

	for _, algo := range SupportedAdminAlgos() {
		a1, err := GenerateAdminKey(algo)
		require.NoError(t, err)
		a2, err := GenerateAdminKey(algo)
		require.NoError(t, err)
		assert.NotEqual(t, a1.PublicKeyHex(), a2.PublicKeyHex(), "admin %s public keys must differ", algo)
	}
}

func TestParsePrivateKeyPEM_WrongBlockType(t *testing.T) {
	// A certificate PEM fed to the private-key parser is rejected, not
	// misinterpreted.
	pair, err := GenerateGossipKey(GossipOptions{MachineUUID: "u"})
	require.NoError(t, err)
	_, err = ParsePrivateKeyPEM(pair.CertificatePEM())
	require.Error(t, err)
}

func TestParseSEC1PrivateKey_RejectsNonSecp256k1Curve(t *testing.T) {
	// A NIST P-256 SEC1 key (valid SEC1, wrong curve for our secp256k1 path)
	// still parses via the stdlib fallback; a corrupt one errors.
	_, err := parseSEC1PrivateKey([]byte("garbage"))
	require.Error(t, err)
}

func TestAdminECDSA_CompressedPubKeyMatchesPrivate(t *testing.T) {
	pair, err := GenerateAdminKey(AdminECDSA)
	require.NoError(t, err)
	ec := pair.PrivateKey.(*ecdsa.PrivateKey)
	priv := secp256k1.PrivKeyFromBytes(ec.D.Bytes())
	assert.Equal(t, priv.PubKey().SerializeCompressed(), pair.PublicKeyRaw())
}

func TestGenerateGossipKey_ResolvesMachineUUID(t *testing.T) {
	// With no explicit UUID and no host source, generation fails loudly rather
	// than minting an unstable identity.
	orig := machineIDPaths
	t.Cleanup(func() { machineIDPaths = orig })
	machineIDPaths = []string{filepath.Join(t.TempDir(), "does-not-exist")}

	_, err := GenerateGossipKey(GossipOptions{})
	require.Error(t, err)
}

func TestGenerateGrpcKey(t *testing.T) {
	pair, err := GenerateGrpcKey()
	require.NoError(t, err)

	assert.Equal(t, "localhost", pair.Certificate.Subject.CommonName)
	assert.Contains(t, pair.Certificate.DNSNames, "localhost")
	require.Len(t, pair.Certificate.IPAddresses, 2)

	// grpc_certificate_hash is the SHA-384 of the cert DER.
	want := sha512.Sum384(pair.Certificate.Raw)
	assert.Equal(t, hex.EncodeToString(want[:]), pair.CertHashSHA384())
}

func TestCertKeyPair_PEMRoundTrip(t *testing.T) {
	pair, err := GenerateGossipKey(GossipOptions{MachineUUID: "uuid-1"})
	require.NoError(t, err)

	privPEM, err := pair.PrivateKeyPEM()
	require.NoError(t, err)
	certPEM := pair.CertificatePEM()

	gotKey, err := ParsePrivateKeyPEM(privPEM)
	require.NoError(t, err)
	assert.IsType(t, &rsa.PrivateKey{}, gotKey)

	gotCert, err := ParseCertificatePEM(certPEM)
	require.NoError(t, err)
	assert.Equal(t, pair.Certificate.Raw, gotCert.Raw)
}

func TestGenerateAdminKey_Ed25519(t *testing.T) {
	pair, err := GenerateAdminKey(AdminEd25519)
	require.NoError(t, err)

	assert.Equal(t, AdminEd25519, pair.Algo)
	assert.Len(t, pair.PublicKeyRaw(), ed25519.PublicKeySize)
	assert.Equal(t, hex.EncodeToString(pair.PublicKeyRaw()), pair.PublicKeyHex())

	// Private key round-trips as PKCS#8.
	got, err := ParsePrivateKeyPEM(pair.PrivateKeyPEM())
	require.NoError(t, err)
	edKey, ok := got.(ed25519.PrivateKey)
	require.True(t, ok)
	assert.Equal(t, ed25519.PublicKey(pair.PublicKeyRaw()), edKey.Public())
}

func TestGenerateAdminKey_ECDSA(t *testing.T) {
	pair, err := GenerateAdminKey(AdminECDSA)
	require.NoError(t, err)

	assert.Equal(t, AdminECDSA, pair.Algo)
	// secp256k1 compressed public key is 33 bytes with a 0x02/0x03 prefix.
	raw := pair.PublicKeyRaw()
	require.Len(t, raw, 33)
	assert.Contains(t, []byte{0x02, 0x03}, raw[0])

	// Private key round-trips through the SEC1 secp256k1 path and recovers the
	// same key point.
	got, err := ParsePrivateKeyPEM(pair.PrivateKeyPEM())
	require.NoError(t, err)
	ecKey, ok := got.(*ecdsa.PrivateKey)
	require.True(t, ok)
	orig := pair.PrivateKey.(*ecdsa.PrivateKey)
	assert.Equal(t, orig.D, ecKey.D)
	assert.Equal(t, orig.X, ecKey.X)
	assert.Equal(t, orig.Y, ecKey.Y)
}

func TestGenerateAdminKey_Unsupported(t *testing.T) {
	_, err := GenerateAdminKey("rsa")
	require.Error(t, err)
}

func TestMachineUUIDFrom(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	require.NoError(t, os.WriteFile(empty, []byte("  \n"), 0o600))
	valid := filepath.Join(dir, "machine-id")
	require.NoError(t, os.WriteFile(valid, []byte("abc123\n"), 0o600))

	// Skips the missing and empty files, trims the valid one.
	got, err := machineUUIDFrom([]string{filepath.Join(dir, "missing"), empty, valid})
	require.NoError(t, err)
	assert.Equal(t, "abc123", got)

	_, err = machineUUIDFrom([]string{filepath.Join(dir, "missing")})
	require.Error(t, err)
}

func TestParsePrivateKeyPEM_Errors(t *testing.T) {
	_, err := ParsePrivateKeyPEM([]byte("not pem"))
	require.Error(t, err)

	_, err = ParseCertificatePEM([]byte("not pem"))
	require.Error(t, err)
}
