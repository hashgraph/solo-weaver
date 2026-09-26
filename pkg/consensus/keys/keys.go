// SPDX-License-Identifier: Apache-2.0

// Package keys is the pure-Go cryptographic core for consensus-node key
// material. It generates the three kinds of keys a Hiero consensus node needs
// and encodes them as enhanced PEM (separate private key + public
// certificate/key), matching what solo-cli produces and what the
// hiero-consensus-node EnhancedKeyStoreLoader consumes:
//
//   - Gossip signing key: RSA-3072, self-signed X.509 with SHA384WithRSA. This
//     is the node's long-lived network identity (roster gossip_ca_certificate).
//     The EC agreement key is auto-derived by the platform at boot, so it is not
//     generated here.
//   - gRPC TLS key: self-signed X.509, CN=localhost. HAProxy terminates client
//     TLS with its own cert and re-encrypts to the node with `ssl verify none`,
//     so this cert is never hostname/CA-validated — only its SHA-384 hash
//     (grpc_certificate_hash) is published to the roster.
//   - Admin key: Ed25519 or ECDSA (secp256k1). The private key stays off-cluster
//     in the operator's wallet; only the raw public key is pushed to Kubernetes.
//
// The package is intentionally free of Kubernetes, filesystem, and CLI
// dependencies: it deals in in-memory keys and PEM/DER byte slices only. Callers
// (the `consensus node keys generate/import/verify/init` commands) own I/O and
// secret materialization. All randomness comes from crypto/rand (CSPRNG); the
// hiero reference generator uses a deterministic RNG for tests, which must never
// be copied here.
package keys

import (
	"crypto"
	"crypto/rand"
	"crypto/sha512"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net"
	"time"

	"github.com/joomcode/errorx"
)

const (
	// GossipKeyBits is the RSA modulus size for the gossip signing key. RSA-3072
	// matches the hiero-consensus-node reference key generator.
	GossipKeyBits = 3072

	// GrpcKeyBits is the RSA modulus size for the self-signed gRPC TLS key.
	GrpcKeyBits = 3072

	// certValidYears is the self-signed certificate lifetime. These certs are
	// long-lived identity anchors — rotation is a network-coordinated DAB
	// operation, not a certificate-expiry event — so we mirror the 100-year
	// validity of the hiero reference generator (openssl -days 36500).
	certValidYears = 100

	// PEMTypeCertificate is the PEM block type for an X.509 certificate.
	PEMTypeCertificate = "CERTIFICATE"
	// PEMTypePKCS8 is the PEM block type for a PKCS#8-encoded private key
	// (RSA gossip/gRPC keys and Ed25519 admin keys).
	PEMTypePKCS8 = "PRIVATE KEY"
	// PEMTypeECPrivate is the PEM block type for a SEC1-encoded EC private key
	// (secp256k1 admin keys, which the stdlib cannot marshal as PKCS#8).
	PEMTypeECPrivate = "EC PRIVATE KEY"
)

// CertKeyPair is an X.509-certificate-bearing keypair: a private key plus its
// self-signed certificate. It backs both the gossip signing key and the gRPC
// TLS key.
type CertKeyPair struct {
	// PrivateKey is the in-memory signer (an *rsa.PrivateKey for keys generated
	// here; any crypto.Signer for parsed keys).
	PrivateKey crypto.Signer
	// Certificate is the parsed self-signed certificate. Certificate.Raw holds
	// the DER used for PEM encoding and the SHA-384 hash.
	Certificate *x509.Certificate
}

// PrivateKeyPEM returns the private key as a PKCS#8 PEM block (0600-worthy).
func (p *CertKeyPair) PrivateKeyPEM() ([]byte, error) {
	return marshalPKCS8PEM(p.PrivateKey)
}

// CertificatePEM returns the self-signed certificate as a PEM block.
func (p *CertKeyPair) CertificatePEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: PEMTypeCertificate, Bytes: p.Certificate.Raw})
}

// CertHashSHA384 returns the lowercase-hex SHA-384 digest of the certificate
// DER. For the gRPC TLS cert this is the roster's grpc_certificate_hash.
func (p *CertKeyPair) CertHashSHA384() string {
	sum := sha512.Sum384(p.Certificate.Raw)
	return hex.EncodeToString(sum[:])
}

// selfSignedCert builds and self-signs an X.509 certificate for key, returning
// the parsed certificate (with Raw populated).
func selfSignedCert(key crypto.Signer, subject pkix.Name, sigAlgo x509.SignatureAlgorithm, dnsNames []string, ipAddresses []net.IP) (*x509.Certificate, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, errorx.Decorate(err, "generate certificate serial number")
	}

	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               subject,
		NotBefore:             now.Add(-1 * time.Hour),
		NotAfter:              now.AddDate(certValidYears, 0, 0),
		SignatureAlgorithm:    sigAlgo,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              dnsNames,
		IPAddresses:           ipAddresses,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		return nil, errorx.Decorate(err, "self-sign certificate")
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, errorx.Decorate(err, "parse self-signed certificate")
	}
	return cert, nil
}

// marshalPKCS8DER encodes a private key as PKCS#8 DER.
func marshalPKCS8DER(key crypto.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "marshal private key to PKCS#8")
	}
	return der, nil
}

// marshalPKCS8PEM encodes a private key as a PKCS#8 PEM block.
func marshalPKCS8PEM(key crypto.PrivateKey) ([]byte, error) {
	der, err := marshalPKCS8DER(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: PEMTypePKCS8, Bytes: der}), nil
}
