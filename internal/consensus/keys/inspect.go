// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	corekeys "github.com/hashgraph/solo-weaver/pkg/consensus/keys"
	"github.com/joomcode/errorx"
)

// CertInfo is the public description of an X.509 certificate, equivalent to the
// useful parts of `openssl x509 -text -noout`.
type CertInfo struct {
	Subject            string    `json:"subject"`
	Issuer             string    `json:"issuer"`
	SelfSigned         bool      `json:"selfSigned"`
	SerialNumber       string    `json:"serialNumber"`
	NotBefore          time.Time `json:"notBefore"`
	NotAfter           time.Time `json:"notAfter"`
	DaysRemaining      int       `json:"daysRemaining"`
	SignatureAlgorithm string    `json:"signatureAlgorithm"`
	PublicKeyAlgorithm string    `json:"publicKeyAlgorithm"`
	PublicKeyBits      int       `json:"publicKeyBits,omitempty"`
	DNSNames           []string  `json:"dnsNames,omitempty"`
	IPAddresses        []string  `json:"ipAddresses,omitempty"`
	KeyUsage           []string  `json:"keyUsage,omitempty"`
	ExtKeyUsage        []string  `json:"extKeyUsage,omitempty"`
	IsCA               bool      `json:"isCA"`
	SHA256             string    `json:"sha256"`
	SHA384             string    `json:"sha384"`
}

// AdminPublicInfo describes an admin public key.
type AdminPublicInfo struct {
	Algorithm    string `json:"algorithm"`
	PublicKeyHex string `json:"publicKeyHex"`
}

// KeyInspection is the inspection result for one key type or file.
type KeyInspection struct {
	// Key is the key type: "gossip", "grpc", "admin", or "file" for --file.
	Key string `json:"key"`
	// File is the name of the inspected file (e.g. "hedera.crt").
	File string `json:"file"`
	// Kind says what the file is, e.g. "gossip certificate".
	Kind        string           `json:"kind"`
	Certificate *CertInfo        `json:"certificate,omitempty"`
	AdminPublic *AdminPublicInfo `json:"adminPublic,omitempty"`
	// Error explains why the key could not be inspected (missing or unparseable).
	Error string `json:"error,omitempty"`
}

// InspectReport aggregates the inspections for a source.
type InspectReport struct {
	Source string          `json:"source"`
	Keys   []KeyInspection `json:"keys"`
	// Missing lists the selected files that were not found; they are skipped,
	// not treated as errors.
	Missing []string `json:"missing,omitempty"`
}

// HasErrors reports whether any key could not be inspected.
func (r *InspectReport) HasErrors() bool {
	for _, k := range r.Keys {
		if k.Error != "" {
			return true
		}
	}
	return false
}

// InspectDir describes the selected key material in a folder.
func InspectDir(dir string, selectors Selectors) (*InspectReport, error) {
	sel := NormalizeSelectors(selectors)
	m, err := loadDir(dir, sel, false)
	if err != nil {
		return nil, err
	}
	return inspect(m, sel, fmt.Sprintf("folder %s", dir), time.Now()), nil
}

// InspectFile describes a single certificate (PEM) or admin public key (hex) file.
func InspectFile(path string) (*InspectReport, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errorx.ExternalError.Wrap(err, "read %q", path)
	}
	ki := KeyInspection{Key: "file", File: filepath.Base(path)}
	if strings.Contains(string(raw), "-----BEGIN") {
		ki.Kind = knownKind(ki.File, "certificate")
		cert, perr := corekeys.ParseCertificatePEM(raw)
		if perr != nil {
			ki.Error = perr.Error()
		} else {
			ki.Certificate = describeCert(cert, time.Now())
		}
	} else if info, perr := describeAdminPub(raw); perr != nil {
		ki.Error = "not a PEM certificate or a hex admin public key"
	} else {
		ki.Kind = "admin public key"
		ki.AdminPublic = info
	}
	return &InspectReport{Source: "file " + path, Keys: []KeyInspection{ki}}, nil
}

// kindFor returns the display label for a key type's certificate.
func kindFor(key string) string {
	if key == "grpc" {
		return "gRPC TLS certificate"
	}
	return key + " certificate"
}

// knownKind labels a --file by its well-known generated name, else fallback.
func knownKind(file, fallback string) string {
	switch file {
	case FileGossipCert:
		return kindFor("gossip")
	case FileGrpcCert:
		return kindFor("grpc")
	}
	return fallback
}

func inspect(m *material, sel Selectors, source string, now time.Time) *InspectReport {
	r := &InspectReport{Source: source}
	add := func(present bool, file string, ki func() KeyInspection) {
		if !present {
			r.Missing = append(r.Missing, file)
			return
		}
		r.Keys = append(r.Keys, ki())
	}
	if sel.Gossip {
		add(len(m.gossipCert) > 0, FileGossipCert, func() KeyInspection {
			return inspectCertKey("gossip", FileGossipCert, m.gossipCert, now)
		})
	}
	if sel.Grpc {
		add(len(m.grpcCert) > 0, FileGrpcCert, func() KeyInspection {
			return inspectCertKey("grpc", FileGrpcCert, m.grpcCert, now)
		})
	}
	if sel.Admin {
		add(len(m.adminPub) > 0, FileAdminPublic, func() KeyInspection { return inspectAdmin(m) })
	}
	return r
}

func inspectCertKey(name, file string, certPEM []byte, now time.Time) KeyInspection {
	ki := KeyInspection{Key: name, File: file, Kind: kindFor(name)}
	cert, err := corekeys.ParseCertificatePEM(certPEM)
	if err != nil {
		ki.Error = fmt.Sprintf("certificate does not parse: %v", err)
		return ki
	}
	ki.Certificate = describeCert(cert, now)

	return ki
}

func inspectAdmin(m *material) KeyInspection {
	ki := KeyInspection{Key: "admin", File: FileAdminPublic, Kind: "admin public key"}
	info, err := describeAdminPub(m.adminPub)
	if err != nil {
		ki.Error = err.Error()
		return ki
	}
	ki.AdminPublic = info

	return ki
}

func describeAdminPub(raw []byte) (*AdminPublicInfo, error) {
	pub, algo, err := decodeAdminPub(raw)
	if err != nil {
		return nil, err
	}
	return &AdminPublicInfo{Algorithm: string(algo), PublicKeyHex: hex.EncodeToString(pub)}, nil
}

func describeCert(c *x509.Certificate, now time.Time) *CertInfo {
	sum256 := sha256.Sum256(c.Raw)
	sum384 := sha512.Sum384(c.Raw)
	algo, bits := publicKeyShape(c.PublicKey)
	info := &CertInfo{
		Subject:            c.Subject.String(),
		Issuer:             c.Issuer.String(),
		SelfSigned:         c.Subject.String() == c.Issuer.String(),
		SerialNumber:       c.SerialNumber.Text(16),
		NotBefore:          c.NotBefore.UTC(),
		NotAfter:           c.NotAfter.UTC(),
		DaysRemaining:      int(c.NotAfter.Sub(now).Hours() / 24),
		SignatureAlgorithm: c.SignatureAlgorithm.String(),
		PublicKeyAlgorithm: algo,
		PublicKeyBits:      bits,
		DNSNames:           c.DNSNames,
		KeyUsage:           keyUsageNames(c.KeyUsage),
		IsCA:               c.IsCA,
		SHA256:             hex.EncodeToString(sum256[:]),
		SHA384:             hex.EncodeToString(sum384[:]),
	}
	for _, ip := range c.IPAddresses {
		info.IPAddresses = append(info.IPAddresses, ip.String())
	}
	for _, eku := range c.ExtKeyUsage {
		info.ExtKeyUsage = append(info.ExtKeyUsage, extKeyUsageName(eku))
	}
	return info
}

func publicKeyShape(pub crypto.PublicKey) (string, int) {
	switch k := pub.(type) {
	case *rsa.PublicKey:
		return "RSA", k.N.BitLen()
	case *ecdsa.PublicKey:
		return "ECDSA", k.Curve.Params().BitSize
	case ed25519.PublicKey:
		return "Ed25519", 256
	default:
		return fmt.Sprintf("%T", pub), 0
	}
}

func keyUsageNames(u x509.KeyUsage) []string {
	table := []struct {
		bit  x509.KeyUsage
		name string
	}{
		{x509.KeyUsageDigitalSignature, "Digital Signature"},
		{x509.KeyUsageContentCommitment, "Content Commitment"},
		{x509.KeyUsageKeyEncipherment, "Key Encipherment"},
		{x509.KeyUsageDataEncipherment, "Data Encipherment"},
		{x509.KeyUsageKeyAgreement, "Key Agreement"},
		{x509.KeyUsageCertSign, "Certificate Sign"},
		{x509.KeyUsageCRLSign, "CRL Sign"},
		{x509.KeyUsageEncipherOnly, "Encipher Only"},
		{x509.KeyUsageDecipherOnly, "Decipher Only"},
	}
	var out []string
	for _, e := range table {
		if u&e.bit != 0 {
			out = append(out, e.name)
		}
	}
	return out
}

func extKeyUsageName(u x509.ExtKeyUsage) string {
	switch u {
	case x509.ExtKeyUsageServerAuth:
		return "TLS Web Server Authentication"
	case x509.ExtKeyUsageClientAuth:
		return "TLS Web Client Authentication"
	case x509.ExtKeyUsageCodeSigning:
		return "Code Signing"
	default:
		return fmt.Sprintf("ExtKeyUsage(%d)", int(u))
	}
}
