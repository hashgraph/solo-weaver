// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"os"
	"path/filepath"

	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/joomcode/errorx"
)

const (
	// dirPerm is the permission for a key output directory (owner-only).
	dirPerm os.FileMode = 0o700
	// privatePerm is the permission for private key material (owner read/write).
	privatePerm os.FileMode = 0o600
	// publicPerm is the permission for public certs/keys.
	publicPerm os.FileMode = 0o644
)

// WriteToDir writes the key set to dir with neutral, index-free file names and
// tight permissions (0600 on private material).
//
// It does not write a combined artifacts file: the values an operator needs for
// the DAB nodeCreate transaction are the gossip certificate s-public.pem plus
// the SHA-384 hash of the gRPC certificate, which the caller surfaces from the
// key set (CertKeyPair.CertHashSHA384) rather than persisting redundantly. The
// admin key is created in an external wallet and is never generated here.
func (ks *NodeKeySet) WriteToDir(dir string) error {
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return errorx.ExternalError.Wrap(err, "create key output directory %q", dir)
	}

	if ks.Gossip != nil {
		privPEM, err := ks.Gossip.PrivateKeyPEM()
		if err != nil {
			return errorx.Decorate(err, "encode gossip private key")
		}
		if err := writeFile(dir, FileGossipPrivate, privPEM, privatePerm); err != nil {
			return err
		}
		if err := writeFile(dir, FileGossipCert, ks.Gossip.CertificatePEM(), publicPerm); err != nil {
			return err
		}
	}

	if ks.Grpc != nil {
		privPEM, err := ks.Grpc.PrivateKeyPEM()
		if err != nil {
			return errorx.Decorate(err, "encode gRPC private key")
		}
		if err := writeFile(dir, FileGrpcKey, privPEM, privatePerm); err != nil {
			return err
		}
		if err := writeFile(dir, FileGrpcCert, ks.Grpc.CertificatePEM(), publicPerm); err != nil {
			return err
		}
	}

	return nil
}

// writeFile atomically writes payload to dir/name with perm (temp file + rename),
// so a pre-existing file never holds the new payload under its old mode.
func writeFile(dir, name string, payload []byte, perm os.FileMode) error {
	path := filepath.Join(dir, name)
	if err := fsx.AtomicWriteFile(path, payload, perm); err != nil {
		return errorx.Decorate(err, "write %q", path)
	}
	return nil
}
