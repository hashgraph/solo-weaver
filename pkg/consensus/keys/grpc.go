// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"net"
	"slices"
	"strings"
	"unicode"

	"github.com/joomcode/errorx"
)

// GrpcOptions customises the gRPC TLS certificate. The zero value yields the
// default certificate.
type GrpcOptions struct {
	// CommonName is the certificate subject (and so issuer) CN. Empty keeps the
	// default "localhost".
	CommonName string
	// Organization is the optional subject O (and so issuer O). Empty omits it.
	Organization string
	// DNSNames are extra DNS SANs added after the default "localhost".
	DNSNames []string
	// IPAddresses are extra IP SANs added after the default loopback addresses.
	IPAddresses []net.IP
	// ValidityDays is the certificate lifetime in days. Zero keeps the default
	// 100-year lifetime.
	ValidityDays int
}

// GenerateGrpcKey creates the gRPC TLS key: an RSA-3072 keypair with a
// self-signed X.509 certificate (CN=localhost unless opts.CommonName is set). HAProxy terminates client TLS
// with its own certificate and re-encrypts to the node with `ssl verify none`,
// so this certificate is never hostname- or CA-validated by any peer; only its
// SHA-384 hash (grpc_certificate_hash, see CertHashSHA384) is published to the
// roster. The localhost and loopback SANs are always included so the node can
// present a well-formed cert to the local proxy; opts adds to them.
func GenerateGrpcKey(opts GrpcOptions) (*CertKeyPair, error) {
	if opts.ValidityDays < 0 || opts.ValidityDays > MaxGrpcValidityDays {
		return nil, errorx.IllegalArgument.New("gRPC certificate validity must be between 1 and %d days, got %d", MaxGrpcValidityDays, opts.ValidityDays)
	}

	cn, err := NormalizeGrpcCommonName(opts.CommonName)
	if err != nil {
		return nil, err
	}

	org, err := NormalizeGrpcOrganization(opts.Organization)
	if err != nil {
		return nil, err
	}

	key, err := rsa.GenerateKey(rand.Reader, GrpcKeyBits)
	if err != nil {
		return nil, errorx.Decorate(err, "generate RSA-%d gRPC TLS key", GrpcKeyBits)
	}

	dnsNames := appendUnique([]string{"localhost"}, opts.DNSNames...)
	ips := []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}
	for _, ip := range opts.IPAddresses {
		if !containsIP(ips, ip) {
			ips = append(ips, ip)
		}
	}

	subject := pkix.Name{CommonName: cn}
	if org != "" {
		subject.Organization = []string{org}
	}

	cert, err := selfSignedCert(key, subject, x509.SHA384WithRSA,
		dnsNames, ips, opts.ValidityDays)
	if err != nil {
		return nil, errorx.Decorate(err, "self-sign gRPC TLS certificate")
	}
	return &CertKeyPair{PrivateKey: key, Certificate: cert}, nil
}

// NormalizeGrpcCommonName validates a custom CN and returns it trimmed, or "localhost" for the empty value.
func NormalizeGrpcCommonName(cn string) (string, error) {
	if cn == "" {
		return "localhost", nil
	}
	return normalizeSubjectField("common name", cn)
}

// NormalizeGrpcOrganization validates a custom organization and returns it
// trimmed, or "" for the empty value (no O in the subject).
func NormalizeGrpcOrganization(org string) (string, error) {
	if org == "" {
		return "", nil
	}
	return normalizeSubjectField("organization", org)
}

// normalizeSubjectField trims v and rejects blank, over-long, or control-character
// values. The 64-character cap is the X.509 bound for both CN and O.
func normalizeSubjectField(label, v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", errorx.IllegalArgument.New("gRPC certificate %s cannot be blank", label)
	}
	if len(v) > MaxSubjectFieldLength {
		return "", errorx.IllegalArgument.New("gRPC certificate %s is %d characters; the maximum is %d", label, len(v), MaxSubjectFieldLength)
	}
	if strings.IndexFunc(v, unicode.IsControl) >= 0 {
		return "", errorx.IllegalArgument.New("gRPC certificate %s cannot contain control characters", label)
	}
	return v, nil
}

func appendUnique(base []string, extra ...string) []string {
	for _, e := range extra {
		if !slices.Contains(base, e) {
			base = append(base, e)
		}
	}
	return base
}

func containsIP(list []net.IP, ip net.IP) bool {
	return slices.ContainsFunc(list, func(x net.IP) bool { return x.Equal(ip) })
}
