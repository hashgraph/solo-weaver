// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"crypto"
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/joomcode/errorx"
)

// ParsePrivateKeyPEM decodes a private key from PEM. It accepts PKCS#8 blocks
// (RSA gossip/gRPC keys, Ed25519 admin keys) and SEC1 "EC PRIVATE KEY" blocks
// (secp256k1 admin keys). It is the counterpart to CertKeyPair.PrivateKeyPEM /
// AdminKeyPair.PrivateKeyPEM and underpins `keys import` and `keys verify`.
func ParsePrivateKeyPEM(data []byte) (crypto.Signer, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errorx.IllegalArgument.New("no PEM block found in private key data")
	}

	switch block.Type {
	case PEMTypePKCS8:
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, errorx.IllegalArgument.Wrap(err, "parse PKCS#8 private key")
		}
		signer, ok := key.(crypto.Signer)
		if !ok {
			return nil, errorx.IllegalArgument.New("PKCS#8 key of type %T is not a signer", key)
		}
		return signer, nil
	case PEMTypeECPrivate:
		return parseSEC1PrivateKey(block.Bytes)
	default:
		return nil, errorx.IllegalArgument.New("unsupported private key PEM block type %q", block.Type)
	}
}

// parseSEC1PrivateKey parses a SEC1 EC private key, supporting the secp256k1
// named curve that the stdlib rejects. NIST-curve SEC1 keys fall back to the
// stdlib parser.
func parseSEC1PrivateKey(der []byte) (crypto.Signer, error) {
	if key, err := x509.ParseECPrivateKey(der); err == nil {
		return key, nil
	}

	var parsed ecPrivateKey
	if _, err := asn1.Unmarshal(der, &parsed); err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "parse SEC1 EC private key")
	}
	if !parsed.NamedCurve.Equal(oidSecp256k1) {
		return nil, errorx.IllegalArgument.New("unsupported EC named curve %v in SEC1 private key", parsed.NamedCurve)
	}
	priv := secp256k1.PrivKeyFromBytes(parsed.PrivateKey)
	return priv.ToECDSA(), nil
}

// ParseCertificatePEM decodes a single X.509 certificate from PEM.
func ParseCertificatePEM(data []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errorx.IllegalArgument.New("no PEM block found in certificate data")
	}
	if block.Type != PEMTypeCertificate {
		return nil, errorx.IllegalArgument.New("expected a %q PEM block, got %q", PEMTypeCertificate, block.Type)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "parse X.509 certificate")
	}
	return cert, nil
}
