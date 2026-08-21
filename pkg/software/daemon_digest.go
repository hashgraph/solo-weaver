// SPDX-License-Identifier: Apache-2.0

package software

import (
	"encoding/hex"
	"strings"

	"github.com/automa-saga/version"
	"github.com/joomcode/errorx"
)

// pinnedDaemonDigest is the SHA-256 of the solo-provisioner-daemon binary that
// was built alongside this binary, stamped at link time by `task build:cli`:
//
//	-X github.com/hashgraph/solo-weaver/pkg/software.pinnedDaemonDigest=<hex>
//
// It anchors the daemon auto-download in bytes produced by the same pipeline run
// as its verifier, so no signing key — and therefore no key rotation — sits on
// the install path.
//
// It is empty in any binary not linked through that task (`go build`, `go run`,
// `go test`), which is why resolution falls back to the published checksum
// rather than treating an empty digest as a match.
var pinnedDaemonDigest string

// daemonChecksumAlgorithm is the digest algorithm for both the link-time pin and
// the published checksum asset. Changing it means re-stamping in taskfiles/cli.yaml.
const daemonChecksumAlgorithm = "sha256"

// sha256HexLen is the length of a hex-encoded SHA-256 digest.
const sha256HexLen = 64

// maxChecksumAssetBytes caps how much of a published checksum asset is read. The
// real asset is one `sha256sum` line; anything larger is not one and must not be
// slurped into memory.
const maxChecksumAssetBytes int64 = 4096

// pinnedDigestFor returns the link-time daemon digest when it applies to version.
//
// Only the co-released version qualifies: the digest was computed from the daemon
// built in the same pipeline run as this binary, so it attests to that version
// and no other. Any other version the operator names has to be verified against
// its own published checksum.
func pinnedDigestFor(requested string) (string, bool) {
	if pinnedDaemonDigest == "" || requested == "" {
		return "", false
	}
	if requested != ownVersion() {
		return "", false
	}
	return pinnedDaemonDigest, true
}

// ownVersion reports the version stamped into this binary at link time — the
// same value that supplies the default for --daemon-version, which is what makes
// the pinned digest apply to the default install path.
func ownVersion() string {
	return version.Get().Version
}

// parseChecksumAsset extracts the hex digest from a published `<binary>.sha256`
// asset, whose contents are a single `sha256sum` line: "<hex>  <filename>".
//
// Only that exact shape is accepted. A multi-entry body is rejected rather than
// resolved to its first line: the entry matching this artifact need not be the
// first, so taking one would be a guess. A bare filename, an HTML error page
// served with a 200, and a truncated download must fail here too, as malformed
// input rather than as a digest mismatch.
func parseChecksumAsset(content []byte, source string) (string, error) {
	var line string
	for _, candidate := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if line != "" {
			return "", errorx.IllegalFormat.New(
				"checksum asset %s holds more than one entry, expected a single %s line",
				source, daemonChecksumAlgorithm)
		}
		line = candidate
	}
	if line == "" {
		return "", errorx.IllegalFormat.New("checksum asset %s is empty", source)
	}

	// A digest, optionally followed by the filename it was computed from.
	fields := strings.Fields(line)
	if len(fields) > 2 {
		return "", errorx.IllegalFormat.New(
			"checksum asset %s has %d fields, expected a %s digest and an optional filename",
			source, len(fields), daemonChecksumAlgorithm)
	}

	digest := strings.ToLower(fields[0])
	if len(digest) != sha256HexLen {
		return "", errorx.IllegalFormat.New(
			"checksum asset %s does not start with a %s digest: got %d characters, want %d",
			source, daemonChecksumAlgorithm, len(digest), sha256HexLen)
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", errorx.IllegalFormat.Wrap(err, "checksum asset %s is not hex-encoded", source)
	}
	return digest, nil
}
