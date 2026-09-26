// SPDX-License-Identifier: Apache-2.0

// Package keys is the orchestration layer for the `consensus keys` command
// family. It composes the pure-Go primitives in pkg/consensus/keys into
// per-node key sets and materializes them as on-disk PEM folders whose file
// names match the Kubernetes secret inner-keys, so `keys import` can map a
// generated folder to secrets one-to-one.
package keys

import (
	"os"
	"path/filepath"

	corekeys "github.com/hashgraph/solo-weaver/pkg/consensus/keys"
	"github.com/joomcode/errorx"
)

// Neutral, index-free artifact file names. They double as the Kubernetes secret
// inner-key names (see internal/consensus/keys import in #1167), so a generated
// folder maps to secrets without renaming. The consensus node's on-disk
// node<N+1> filenames are synthesized by the operator at mount, never here.
const (
	FileGossipPrivate = "s-private.pem"
	FileGossipCert    = "s-public.pem"
	FileGrpcKey       = "hedera.key"
	FileGrpcCert      = "hedera.crt"
	FileAdminPrivate  = "admin.key"
	FileAdminPublic   = "admin.pub"
)

// Selectors chooses which key types to generate. A zero Selectors (all false) is
// treated as "all" by NormalizeSelectors.
type Selectors struct {
	Gossip bool
	Grpc   bool
	Admin  bool
}

// NormalizeSelectors returns s unchanged if any selector is set, otherwise a
// Selectors with all three enabled ("no selector flags" means "generate all").
func NormalizeSelectors(s Selectors) Selectors {
	if !s.Gossip && !s.Grpc && !s.Admin {
		return Selectors{Gossip: true, Grpc: true, Admin: true}
	}
	return s
}

// Any reports whether at least one key type is selected.
func (s Selectors) Any() bool {
	return s.Gossip || s.Grpc || s.Admin
}

// Minus returns the types set in s but not in other.
func (s Selectors) Minus(other Selectors) Selectors {
	return Selectors{
		Gossip: s.Gossip && !other.Gossip,
		Grpc:   s.Grpc && !other.Grpc,
		Admin:  s.Admin && !other.Admin,
	}
}

// ExistingSelectors reports which key types already have material in dir, keyed
// off each type's private marker file. Used to make generation idempotent.
func ExistingSelectors(dir string) Selectors {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	return Selectors{
		Gossip: exists(FileGossipPrivate),
		Grpc:   exists(FileGrpcKey),
		Admin:  exists(FileAdminPublic),
	}
}

// GenerateOptions parameterizes a single node's key generation.
type GenerateOptions struct {
	// Selectors chooses which key types to generate (normalized to "all" if empty).
	Selectors Selectors
	// Identity is the optional human label recorded in the gossip cert OU/SAN.
	Identity string
	// MachineID overrides the gossip cert CN. When empty the host machine UUID is
	// resolved via pkg/consensus/keys.MachineUUID.
	MachineID string
	// AdminAlgo selects the admin key algorithm (defaults to Ed25519 when empty).
	AdminAlgo corekeys.AdminAlgo
}

// NodeKeySet holds the generated material for one node. Fields are nil for key
// types the selectors excluded.
type NodeKeySet struct {
	Gossip *corekeys.CertKeyPair
	Grpc   *corekeys.CertKeyPair
	Admin  *corekeys.AdminKeyPair
}

// Generate produces the selected key material for one node. It is node-id-free:
// the gossip cert CN is the machine UUID and the gRPC cert CN is localhost, so
// the same material is valid before the network assigns a node-id at DAB join.
func Generate(opts GenerateOptions) (*NodeKeySet, error) {
	sel := NormalizeSelectors(opts.Selectors)
	set := &NodeKeySet{}

	if sel.Gossip {
		gossip, err := corekeys.GenerateGossipKey(corekeys.GossipOptions{
			MachineUUID: opts.MachineID,
			Identity:    opts.Identity,
		})
		if err != nil {
			return nil, errorx.Decorate(err, "generate gossip signing key")
		}
		set.Gossip = gossip
	}

	if sel.Grpc {
		grpc, err := corekeys.GenerateGrpcKey()
		if err != nil {
			return nil, errorx.Decorate(err, "generate gRPC TLS key")
		}
		set.Grpc = grpc
	}

	if sel.Admin {
		algo := opts.AdminAlgo
		if algo == "" {
			algo = corekeys.AdminEd25519
		}
		admin, err := corekeys.GenerateAdminKey(algo)
		if err != nil {
			return nil, errorx.Decorate(err, "generate admin key")
		}
		set.Admin = admin
	}

	return set, nil
}
