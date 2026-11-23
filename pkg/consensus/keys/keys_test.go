// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"bytes"
	"crypto/rsa"
	"crypto/sha512"
	"crypto/x509"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	pair, err := GenerateGrpcKey(GrpcOptions{})
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

	t1, err := GenerateGrpcKey(GrpcOptions{})
	require.NoError(t, err)
	t2, err := GenerateGrpcKey(GrpcOptions{})
	require.NoError(t, err)
	assert.NotEqual(t, t1.CertHashSHA384(), t2.CertHashSHA384(), "gRPC cert hashes must differ")

}

func TestParsePrivateKeyPEM_WrongBlockType(t *testing.T) {
	// A certificate PEM fed to the private-key parser is rejected, not
	// misinterpreted.
	pair, err := GenerateGossipKey(GossipOptions{MachineUUID: "u"})
	require.NoError(t, err)
	_, err = ParsePrivateKeyPEM(pair.CertificatePEM())
	require.Error(t, err)
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
	pair, err := GenerateGrpcKey(GrpcOptions{})
	require.NoError(t, err)

	assert.Equal(t, "localhost", pair.Certificate.Subject.CommonName)
	assert.Contains(t, pair.Certificate.DNSNames, "localhost")
	require.Len(t, pair.Certificate.IPAddresses, 2)

	// grpc_certificate_hash is the SHA-384 of the PEM-encoded cert (what
	// `openssl dgst -sha384` prints for the .crt file), not of the DER.
	want := sha512.Sum384(pair.CertificatePEM())
	assert.Equal(t, hex.EncodeToString(want[:]), pair.CertHashSHA384())
	assert.Equal(t, pair.CertHashSHA384(), CertPEMHashSHA384(pair.Certificate))
	der := sha512.Sum384(pair.Certificate.Raw)
	assert.NotEqual(t, hex.EncodeToString(der[:]), pair.CertHashSHA384())
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

func TestGenerateGrpcKey_CustomSANsAndValidity(t *testing.T) {
	pair, err := GenerateGrpcKey(GrpcOptions{
		DNSNames:     []string{"node0.example.org", "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("10.1.2.3"), net.IPv4(127, 0, 0, 1)},
		ValidityDays: 365,
	})
	require.NoError(t, err)

	c := pair.Certificate
	assert.Equal(t, []string{"localhost", "node0.example.org"}, c.DNSNames)
	require.Len(t, c.IPAddresses, 3)
	assert.True(t, c.IPAddresses[2].Equal(net.ParseIP("10.1.2.3")))
	assert.Less(t, c.NotAfter.Sub(c.NotBefore), 367*24*time.Hour)
	assert.Greater(t, c.NotAfter.Sub(c.NotBefore), 364*24*time.Hour)
}

func TestGenerateGrpcKey_RejectsBadValidity(t *testing.T) {
	_, err := GenerateGrpcKey(GrpcOptions{ValidityDays: -1})
	require.Error(t, err)
	_, err = GenerateGrpcKey(GrpcOptions{ValidityDays: MaxGrpcValidityDays + 1})
	require.Error(t, err)
}

func TestGenerateGrpcKey_CommonName(t *testing.T) {
	pair, err := GenerateGrpcKey(GrpcOptions{CommonName: "  Example Org "})
	require.NoError(t, err)
	assert.Equal(t, "Example Org", pair.Certificate.Subject.CommonName)
	assert.Equal(t, "Example Org", pair.Certificate.Issuer.CommonName)
	assert.Contains(t, pair.Certificate.DNSNames, "localhost", "localhost SAN stays regardless of CN")

	for _, bad := range []string{"   ", strings.Repeat("a", MaxSubjectFieldLength+1), "bad\x00name", "tab\there"} {
		_, err := GenerateGrpcKey(GrpcOptions{CommonName: bad})
		require.Error(t, err, "%q", bad)
	}
}

func TestGenerateGrpcKey_Organization(t *testing.T) {
	pair, err := GenerateGrpcKey(GrpcOptions{CommonName: "node2", Organization: " Example Org "})
	require.NoError(t, err)
	assert.Equal(t, []string{"Example Org"}, pair.Certificate.Subject.Organization)
	assert.Equal(t, []string{"Example Org"}, pair.Certificate.Issuer.Organization)

	def, err := GenerateGrpcKey(GrpcOptions{})
	require.NoError(t, err)
	assert.Empty(t, def.Certificate.Subject.Organization)

	for _, bad := range []string{"  ", strings.Repeat("o", MaxSubjectFieldLength+1), "bad\x00org"} {
		_, err := GenerateGrpcKey(GrpcOptions{Organization: bad})
		require.Error(t, err, "%q", bad)
	}
}
