// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"net"

	"github.com/joomcode/errorx"
)

// GenerateGrpcKey creates the gRPC TLS key: an RSA-3072 keypair with a
// self-signed X.509 certificate for CN=localhost. HAProxy terminates client TLS
// with its own certificate and re-encrypts to the node with `ssl verify none`,
// so this certificate is never hostname- or CA-validated by any peer; only its
// SHA-384 hash (grpc_certificate_hash, see CertHashSHA384) is published to the
// roster. localhost SANs are included so the node can present a well-formed
// cert to the local proxy.
func GenerateGrpcKey() (*CertKeyPair, error) {
	key, err := rsa.GenerateKey(rand.Reader, GrpcKeyBits)
	if err != nil {
		return nil, errorx.Decorate(err, "generate RSA-%d gRPC TLS key", GrpcKeyBits)
	}

	cert, err := selfSignedCert(key, pkix.Name{CommonName: "localhost"}, x509.SHA384WithRSA,
		[]string{"localhost"}, []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback})
	if err != nil {
		return nil, errorx.Decorate(err, "self-sign gRPC TLS certificate")
	}
	return &CertKeyPair{PrivateKey: key, Certificate: cert}, nil
}
