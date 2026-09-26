// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"crypto"
	"crypto/x509"
	"encoding/pem"

	"github.com/joomcode/errorx"
)

// ParsePrivateKeyPEM decodes a private key from a PKCS#8 PEM block (the RSA
// gossip/gRPC keys). It is the counterpart to CertKeyPair.PrivateKeyPEM and
// underpins `keys import` and `keys verify`.
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
	default:
		return nil, errorx.IllegalArgument.New("unsupported private key PEM block type %q", block.Type)
	}
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
