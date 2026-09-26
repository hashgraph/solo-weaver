// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/asn1"
	"encoding/hex"
	"encoding/pem"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/joomcode/errorx"
)

// AdminAlgo names the signature scheme of an admin key.
type AdminAlgo string

const (
	// AdminEd25519 selects an Ed25519 admin key (32-byte raw public key).
	AdminEd25519 AdminAlgo = "ed25519"
	// AdminECDSA selects an ECDSA secp256k1 admin key (33-byte compressed public
	// key), the curve Hiero/Hedera uses for ECDSA keys.
	AdminECDSA AdminAlgo = "ecdsa"
)

// oidSecp256k1 is the ANSI X9.62 named-curve OID for secp256k1 (1.3.132.0.10),
// used to tag the SEC1 EC private key since the stdlib cannot marshal this curve.
var oidSecp256k1 = asn1.ObjectIdentifier{1, 3, 132, 0, 10}

// AdminKeyPair is a node admin keypair. The private key stays off-cluster in the
// operator's wallet (it signs nodeCreate/nodeUpdate/nodeDelete transactions);
// only the raw public key is pushed to Kubernetes.
type AdminKeyPair struct {
	// Algo is the signature scheme of this key.
	Algo AdminAlgo
	// PrivateKey is the in-memory signer (ed25519.PrivateKey or a secp256k1 key).
	PrivateKey crypto.Signer
	// publicRaw is the wire-format public key: 32 bytes for Ed25519, 33-byte
	// compressed point for secp256k1.
	publicRaw []byte
	// privateDER holds the marshaled private key bytes and the PEM block type.
	privateDER   []byte
	pemBlockType string
}

// SupportedAdminAlgos lists the admin key algorithms this package can generate.
func SupportedAdminAlgos() []AdminAlgo {
	return []AdminAlgo{AdminEd25519, AdminECDSA}
}

// GenerateAdminKey generates an admin keypair for the requested algorithm using
// crypto/rand.
func GenerateAdminKey(algo AdminAlgo) (*AdminKeyPair, error) {
	switch algo {
	case AdminEd25519:
		return generateEd25519Admin()
	case AdminECDSA:
		return generateSecp256k1Admin()
	default:
		return nil, errorx.IllegalArgument.New("unsupported admin key algorithm %q (supported: %v)", algo, SupportedAdminAlgos())
	}
}

func generateEd25519Admin() (*AdminKeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, errorx.Decorate(err, "generate Ed25519 admin key")
	}
	der, err := marshalPKCS8DER(priv)
	if err != nil {
		return nil, err
	}
	return &AdminKeyPair{
		Algo:         AdminEd25519,
		PrivateKey:   priv,
		publicRaw:    append([]byte(nil), pub...),
		privateDER:   der,
		pemBlockType: PEMTypePKCS8,
	}, nil
}

func generateSecp256k1Admin() (*AdminKeyPair, error) {
	priv, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		return nil, errorx.Decorate(err, "generate secp256k1 admin key")
	}
	pubCompressed := priv.PubKey().SerializeCompressed()

	// The stdlib cannot marshal secp256k1 as PKCS#8, so hand-encode SEC1
	// (RFC 5915) with the secp256k1 named-curve OID, the format wallets accept.
	der, err := asn1.Marshal(ecPrivateKey{
		Version:    1,
		PrivateKey: priv.Serialize(),
		NamedCurve: oidSecp256k1,
		PublicKey:  asn1.BitString{Bytes: priv.PubKey().SerializeUncompressed()},
	})
	if err != nil {
		return nil, errorx.Decorate(err, "marshal secp256k1 admin key to SEC1")
	}
	return &AdminKeyPair{
		Algo:         AdminECDSA,
		PrivateKey:   priv.ToECDSA(),
		publicRaw:    pubCompressed,
		privateDER:   der,
		pemBlockType: PEMTypeECPrivate,
	}, nil
}

// ecPrivateKey is the SEC1/RFC 5915 ECPrivateKey ASN.1 structure.
type ecPrivateKey struct {
	Version    int
	PrivateKey []byte
	NamedCurve asn1.ObjectIdentifier `asn1:"optional,explicit,tag:0"`
	PublicKey  asn1.BitString        `asn1:"optional,explicit,tag:1"`
}

// PublicKeyRaw returns the wire-format public key: 32 bytes for Ed25519, or the
// 33-byte compressed point for secp256k1.
func (a *AdminKeyPair) PublicKeyRaw() []byte {
	return append([]byte(nil), a.publicRaw...)
}

// PublicKeyHex returns PublicKeyRaw as lowercase hex. This is what the operator
// pushes to Kubernetes as the AdminPublicKey secret value.
func (a *AdminKeyPair) PublicKeyHex() string {
	return hex.EncodeToString(a.publicRaw)
}

// PrivateKeyPEM returns the admin private key as PEM. Ed25519 keys use a PKCS#8
// block; secp256k1 keys use a SEC1 "EC PRIVATE KEY" block. This material is
// written to the local off-cluster keys folder only — never to Kubernetes.
func (a *AdminKeyPair) PrivateKeyPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: a.pemBlockType, Bytes: a.privateDER})
}
