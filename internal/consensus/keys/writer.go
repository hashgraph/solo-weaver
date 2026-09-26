// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"os"
	"path/filepath"

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
// tight permissions (0600 on private material). The admin private key is written
// to dir only — it is never pushed to Kubernetes.
//
// It does not write a combined artifacts file: the values an operator needs for
// the DAB nodeCreate transaction are the public files themselves (the gossip
// certificate s-public.pem and the admin public key admin.pub) plus the SHA-384
// hash of the gRPC certificate, which the caller surfaces from the key set
// (CertKeyPair.CertHashSHA384) rather than persisting redundantly.
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

	if ks.Admin != nil {
		if err := writeFile(dir, FileAdminPrivate, ks.Admin.PrivateKeyPEM(), privatePerm); err != nil {
			return err
		}
		if err := writeFile(dir, FileAdminPublic, []byte(ks.Admin.PublicKeyHex()+"\n"), publicPerm); err != nil {
			return err
		}
	}

	return nil
}

// writeFile writes payload to dir/name and enforces perm even if the file
// pre-existed with looser bits (os.WriteFile only applies perm on creation).
func writeFile(dir, name string, payload []byte, perm os.FileMode) error {
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, payload, perm); err != nil {
		return errorx.ExternalError.Wrap(err, "write %q", path)
	}
	if err := os.Chmod(path, perm); err != nil {
		return errorx.ExternalError.Wrap(err, "set permissions on %q", path)
	}
	return nil
}
