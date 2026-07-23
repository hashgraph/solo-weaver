// SPDX-License-Identifier: Apache-2.0

// Package codesign verifies that a solo-provisioner release artifact carries a
// valid OpenPGP detached signature made by an embedded trust anchor — the
// project's release signing key. The public key(s) are compiled into the binary
// (//go:embed) so verification needs no network call and cannot be redirected to
// an attacker-controlled key endpoint.
//
// This is the "sig-verify at download" layer described in
// docs/dev/daemon/cli-verification-design.md: a downloaded release binary (e.g.
// the auto-downloaded solo-provisioner-daemon) is trusted only if its detached
// .asc verifies against a key shipped in the verifying binary. A per-artifact
// SHA-256 is unsuitable as an authenticity control because a released binary
// cannot know the checksum of a version published after it; the signing key is
// stable across releases and therefore embeddable.
//
// The verifier matches a signature to a key by issuer key id, so it accepts a
// signature made by the embedded primary key or by any subkey present in the
// embedded block. Validating that a signing subkey chains to an offline primary
// (the rotatable-subkey model in the design doc) is deferred to the self-upgrade
// work that owns that package's fuller key model; the current release key signs
// artifacts with its primary key directly.
package codesign

import (
	"bytes"
	"embed"
	"io"
	"io/fs"
	"sync"

	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/joomcode/errorx"
)

// minDigestBytes is the minimum acceptable signature digest size (256-bit).
// It rejects legacy/weak hashes such as SHA-1 (20 bytes) and MD5 (16 bytes).
const minDigestBytes = 32

// keys holds the embedded release public key block(s). Every *.asc in keys/ is
// loaded, so a key rotation is a matter of adding the new public key block here
// (keep the old one until every deployed binary that signed with it is retired).
//
//go:embed keys/*.asc
var keys embed.FS

var (
	trustedOnce sync.Once
	trustedKeys map[uint64]*packet.PublicKey
	trustedErr  error
)

// loadTrustedKeys parses every embedded *.asc into a key-id -> public key map,
// including subkeys. It is loaded once; a parse failure here means the binary
// was built with a malformed embedded key and is treated as an internal error.
func loadTrustedKeys() (map[uint64]*packet.PublicKey, error) {
	trustedOnce.Do(func() {
		result := map[uint64]*packet.PublicKey{}
		entries, err := fs.ReadDir(keys, "keys")
		if err != nil {
			trustedErr = errorx.InternalError.Wrap(err, "failed to enumerate embedded release keys")
			return
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			data, err := keys.ReadFile("keys/" + entry.Name())
			if err != nil {
				trustedErr = errorx.InternalError.Wrap(err, "failed to read embedded release key %s", entry.Name())
				return
			}
			if err := collectPublicKeys(data, result); err != nil {
				trustedErr = errorx.InternalError.Wrap(err, "failed to parse embedded release key %s", entry.Name())
				return
			}
		}
		if len(result) == 0 {
			trustedErr = errorx.InternalError.New("no embedded release keys found")
			return
		}
		trustedKeys = result
	})
	return trustedKeys, trustedErr
}

// collectPublicKeys parses an armored public key block into out, keyed by key id.
// It reads at the packet level rather than via openpgp.ReadArmoredKeyRing so a
// key with no user-id packet (as served by keys.openpgp.org by fingerprint) still
// loads — ReadArmoredKeyRing rejects a v4 entity without identities.
func collectPublicKeys(armored []byte, out map[uint64]*packet.PublicKey) error {
	block, err := armor.Decode(bytes.NewReader(armored))
	if err != nil {
		return errorx.IllegalFormat.Wrap(err, "malformed public key armor")
	}
	reader := packet.NewReader(block.Body)
	for {
		p, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errorx.IllegalFormat.Wrap(err, "malformed public key packet")
		}
		if pk, ok := p.(*packet.PublicKey); ok {
			out[pk.KeyId] = pk
		}
	}
	return nil
}

// Verify reports whether content carries a valid OpenPGP detached signature
// (armoredSig) made by one of the embedded release keys. It returns nil on a
// good signature and a RejectedOperation error otherwise — an untrusted or
// tampered artifact must not be installed.
//
// content is read to EOF; callers pass the raw artifact bytes (e.g. the
// downloaded binary), not its checksum.
func Verify(content io.Reader, armoredSig io.Reader) error {
	trusted, err := loadTrustedKeys()
	if err != nil {
		return err
	}
	return verifyWith(trusted, content, armoredSig)
}

// verifyWith is the injectable core of Verify: it checks content against
// armoredSig using an explicit key-id -> public key map, so tests can exercise it
// with a generated keypair instead of the embedded trust anchor.
func verifyWith(trusted map[uint64]*packet.PublicKey, content io.Reader, armoredSig io.Reader) error {
	sig, err := readDetachedSignature(armoredSig)
	if err != nil {
		return err
	}
	if sig.IssuerKeyId == nil {
		return errorx.RejectedOperation.New("release signature has no issuer key id")
	}

	pk, ok := trusted[*sig.IssuerKeyId]
	if !ok {
		return errorx.RejectedOperation.New(
			"release signature was made by key %X, which is not an embedded trust anchor", *sig.IssuerKeyId)
	}

	// Reject a signature whose hash is unavailable (would otherwise panic in
	// Hash.New) or weaker than 256-bit (SHA-1/MD5 and friends) before trusting it.
	if !sig.Hash.Available() {
		return errorx.RejectedOperation.New("release signature uses a hash algorithm not linked into this binary")
	}
	if sig.Hash.Size() < minDigestBytes {
		return errorx.RejectedOperation.New(
			"release signature hash is too weak: %d-bit digest, require at least 256-bit", sig.Hash.Size()*8)
	}

	h := sig.Hash.New()
	if _, err := io.Copy(h, content); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to read artifact for signature verification")
	}

	if err := pk.VerifySignature(h, sig); err != nil {
		return errorx.RejectedOperation.Wrap(err, "release signature verification failed")
	}
	return nil
}

// readDetachedSignature decodes an armored detached signature into its packet.
func readDetachedSignature(armoredSig io.Reader) (*packet.Signature, error) {
	block, err := armor.Decode(armoredSig)
	if err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "malformed signature armor")
	}
	p, err := packet.NewReader(block.Body).Next()
	if err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "failed to read signature packet")
	}
	sig, ok := p.(*packet.Signature)
	if !ok {
		return nil, errorx.IllegalFormat.New("expected an OpenPGP signature packet, got %T", p)
	}
	return sig, nil
}
