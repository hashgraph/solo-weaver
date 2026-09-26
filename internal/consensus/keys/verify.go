// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	corekeys "github.com/hashgraph/solo-weaver/pkg/consensus/keys"
	"github.com/joomcode/errorx"
)

// Finding is the verification result for one key type.
type Finding struct {
	// Key is the key type: "gossip", "grpc", or "admin".
	Key string
	// OK is true when every check for this key type passed.
	OK bool
	// Detail is a human-readable summary (what passed, or why it failed).
	Detail string
}

// VerifyReport aggregates the per-key findings for a source.
type VerifyReport struct {
	// Source describes what was checked (a folder path or a namespace/node).
	Source   string
	Findings []Finding
}

// OK reports whether every finding passed.
func (r *VerifyReport) OK() bool {
	for _, f := range r.Findings {
		if !f.OK {
			return false
		}
	}
	return true
}

// material is the raw key bytes gathered from a source, ready to validate. Fields
// are nil for key types not present/selected.
type material struct {
	gossipPriv, gossipCert []byte
	grpcPriv, grpcCert     []byte
	adminPub, adminPriv    []byte
}

// VerifyDir validates the selected key material in a folder.
func VerifyDir(dir string, selectors Selectors) (*VerifyReport, error) {
	sel := NormalizeSelectors(selectors)
	m := &material{}

	read := func(name string) ([]byte, error) {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, errorx.ExternalError.Wrap(err, "read %q", filepath.Join(dir, name))
		}
		return b, nil
	}

	var err error
	if sel.Gossip {
		if m.gossipPriv, err = read(FileGossipPrivate); err != nil {
			return nil, err
		}
		if m.gossipCert, err = read(FileGossipCert); err != nil {
			return nil, err
		}
	}
	if sel.Grpc {
		if m.grpcPriv, err = read(FileGrpcKey); err != nil {
			return nil, err
		}
		if m.grpcCert, err = read(FileGrpcCert); err != nil {
			return nil, err
		}
	}
	if sel.Admin {
		if m.adminPub, err = read(FileAdminPublic); err != nil {
			return nil, err
		}
		// The admin private key is optional (off-cluster); verify correspondence
		// only when it happens to be present in the folder.
		if m.adminPriv, err = read(FileAdminPrivate); err != nil {
			return nil, err
		}
	}

	return validate(m, sel, fmt.Sprintf("folder %s", dir)), nil
}

// VerifyCluster validates the selected per-node secrets in the cluster.
func VerifyCluster(ctx context.Context, client SecretClient, namespace string, nodeID int64, selectors Selectors) (*VerifyReport, error) {
	sel := NormalizeSelectors(selectors)
	names := SecretNamesFor(nodeID)
	m := &material{}

	load := func(secret string) (map[string][]byte, error) {
		data, exists, err := client.GetSecretData(ctx, namespace, secret)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, nil
		}
		return data, nil
	}

	if sel.Gossip {
		if data, err := load(names.Gossip); err != nil {
			return nil, err
		} else if data != nil {
			m.gossipPriv, m.gossipCert = data[FileGossipPrivate], data[FileGossipCert]
		}
	}
	if sel.Grpc {
		if data, err := load(names.Grpc); err != nil {
			return nil, err
		} else if data != nil {
			m.grpcPriv, m.grpcCert = data[FileGrpcKey], data[FileGrpcCert]
		}
	}
	if sel.Admin {
		if data, err := load(names.Admin); err != nil {
			return nil, err
		} else if data != nil {
			m.adminPub = data[FileAdminPublic]
		}
	}

	return validate(m, sel, fmt.Sprintf("namespace %s node%d", namespace, nodeID)), nil
}

func validate(m *material, sel Selectors, source string) *VerifyReport {
	report := &VerifyReport{Source: source}
	if sel.Gossip {
		report.Findings = append(report.Findings, verifyGossip(m))
	}
	if sel.Grpc {
		report.Findings = append(report.Findings, verifyGrpc(m))
	}
	if sel.Admin {
		report.Findings = append(report.Findings, verifyAdmin(m))
	}
	return report
}

func verifyGossip(m *material) Finding {
	f := Finding{Key: "gossip"}
	if len(m.gossipPriv) == 0 || len(m.gossipCert) == 0 {
		f.Detail = "missing gossip signing key or certificate"
		return f
	}
	signer, err := corekeys.ParsePrivateKeyPEM(m.gossipPriv)
	if err != nil {
		f.Detail = fmt.Sprintf("private key does not parse: %v", err)
		return f
	}
	rsaKey, ok := signer.(*rsa.PrivateKey)
	if !ok {
		f.Detail = fmt.Sprintf("gossip signing key must be RSA, got %T", signer)
		return f
	}
	if rsaKey.N.BitLen() != corekeys.GossipKeyBits {
		f.Detail = fmt.Sprintf("gossip key must be RSA-%d, got RSA-%d", corekeys.GossipKeyBits, rsaKey.N.BitLen())
		return f
	}
	cert, err := corekeys.ParseCertificatePEM(m.gossipCert)
	if err != nil {
		f.Detail = fmt.Sprintf("certificate does not parse: %v", err)
		return f
	}
	if !certMatchesKey(cert, signer) {
		f.Detail = "certificate public key does not match the private key"
		return f
	}
	f.OK = true
	f.Detail = fmt.Sprintf("RSA-%d, cert matches key, CN=%s", rsaKey.N.BitLen(), cert.Subject.CommonName)
	return f
}

func verifyGrpc(m *material) Finding {
	f := Finding{Key: "grpc"}
	if len(m.grpcPriv) == 0 || len(m.grpcCert) == 0 {
		f.Detail = "missing gRPC key or certificate"
		return f
	}
	signer, err := corekeys.ParsePrivateKeyPEM(m.grpcPriv)
	if err != nil {
		f.Detail = fmt.Sprintf("private key does not parse: %v", err)
		return f
	}
	cert, err := corekeys.ParseCertificatePEM(m.grpcCert)
	if err != nil {
		f.Detail = fmt.Sprintf("certificate does not parse: %v", err)
		return f
	}
	if !certMatchesKey(cert, signer) {
		f.Detail = "certificate public key does not match the private key"
		return f
	}
	pair := &corekeys.CertKeyPair{PrivateKey: signer, Certificate: cert}
	f.OK = true
	f.Detail = fmt.Sprintf("cert matches key, hash %s", pair.CertHashSHA384())
	return f
}

func verifyAdmin(m *material) Finding {
	f := Finding{Key: "admin"}
	if len(m.adminPub) == 0 {
		f.Detail = "missing admin public key"
		return f
	}
	pub, algo, err := decodeAdminPub(m.adminPub)
	if err != nil {
		f.Detail = err.Error()
		return f
	}

	// If the private key is present (folder), confirm it derives this public key.
	if len(m.adminPriv) > 0 {
		signer, perr := corekeys.ParsePrivateKeyPEM(m.adminPriv)
		if perr != nil {
			f.Detail = fmt.Sprintf("admin private key does not parse: %v", perr)
			return f
		}
		if !adminPubMatchesPriv(pub, algo, signer) {
			f.Detail = "admin public key does not match the private key"
			return f
		}
		f.OK = true
		f.Detail = fmt.Sprintf("%s, public key matches private key", algo)
		return f
	}

	f.OK = true
	f.Detail = string(algo)
	return f
}

// certMatchesKey reports whether cert's public key corresponds to signer's.
func certMatchesKey(cert *x509.Certificate, signer crypto.Signer) bool {
	type equaler interface{ Equal(crypto.PublicKey) bool }
	pub, ok := signer.Public().(equaler)
	return ok && pub.Equal(cert.PublicKey)
}

// decodeAdminPub hex-decodes an admin public key file and infers its algorithm
// from the length: 32 bytes = Ed25519, 33-byte compressed point = secp256k1.
func decodeAdminPub(raw []byte) ([]byte, corekeys.AdminAlgo, error) {
	b, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, "", fmt.Errorf("admin public key is not valid hex: %w", err)
	}
	switch len(b) {
	case ed25519.PublicKeySize:
		return b, corekeys.AdminEd25519, nil
	case 33:
		if b[0] == 0x02 || b[0] == 0x03 {
			return b, corekeys.AdminECDSA, nil
		}
		return nil, "", fmt.Errorf("secp256k1 admin key has invalid compression prefix 0x%02x", b[0])
	default:
		return nil, "", fmt.Errorf("unexpected admin public key length %d (want 32 for ed25519 or 33 for secp256k1)", len(b))
	}
}

// adminPubMatchesPriv reports whether pub is the public key derived from signer.
func adminPubMatchesPriv(pub []byte, algo corekeys.AdminAlgo, signer crypto.Signer) bool {
	switch algo {
	case corekeys.AdminEd25519:
		ed, ok := signer.(ed25519.PrivateKey)
		if !ok {
			return false
		}
		return bytes.Equal(ed.Public().(ed25519.PublicKey), pub)
	case corekeys.AdminECDSA:
		ec, ok := signer.(*ecdsa.PrivateKey)
		if !ok {
			return false
		}
		return bytes.Equal(secp256k1.PrivKeyFromBytes(ec.D.Bytes()).PubKey().SerializeCompressed(), pub)
	default:
		return false
	}
}
