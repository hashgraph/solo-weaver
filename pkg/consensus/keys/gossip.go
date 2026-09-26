// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"

	"github.com/joomcode/errorx"
)

// GossipOptions parameterizes gossip signing key generation.
type GossipOptions struct {
	// MachineUUID is the stable hardware identity used as the certificate CN,
	// decoupling the node's gossip identity from its (deploy-time) node-id. If
	// empty, GenerateGossipKey resolves it from the host via MachineUUID().
	MachineUUID string

	// Identity is an optional human-readable label (hostname or an operator name
	// passed via `--identity`) recorded in the certificate OU and as a DNS SAN
	// for legibility. It is NOT load-bearing: peers are matched by Subject-DN
	// consistency and the address book only checks time-validity, never the DN
	// contents.
	Identity string
}

// GenerateGossipKey creates the gossip signing key: an RSA-3072 keypair with a
// self-signed X.509 certificate signed using SHA384WithRSA. The certificate CN
// is the machine UUID (not the node-id), so the same material is valid before
// the network assigns a node-id during a DAB join. The EC agreement key is
// derived by the platform at boot and is deliberately not produced here.
func GenerateGossipKey(opts GossipOptions) (*CertKeyPair, error) {
	machineUUID := opts.MachineUUID
	if machineUUID == "" {
		resolved, err := MachineUUID()
		if err != nil {
			return nil, errorx.Decorate(err, "resolve machine UUID for gossip certificate CN")
		}
		machineUUID = resolved
	}

	key, err := rsa.GenerateKey(rand.Reader, GossipKeyBits)
	if err != nil {
		return nil, errorx.Decorate(err, "generate RSA-%d gossip signing key", GossipKeyBits)
	}

	subject := pkix.Name{CommonName: machineUUID}
	var dnsNames []string
	if opts.Identity != "" {
		subject.OrganizationalUnit = []string{opts.Identity}
		dnsNames = []string{opts.Identity}
	}

	cert, err := selfSignedCert(key, subject, x509.SHA384WithRSA, dnsNames, nil)
	if err != nil {
		return nil, errorx.Decorate(err, "self-sign gossip certificate")
	}
	return &CertKeyPair{PrivateKey: key, Certificate: cert}, nil
}
