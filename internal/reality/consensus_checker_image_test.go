// SPDX-License-Identifier: Apache-2.0

package reality

import "testing"

// splitLikeCapsule mirrors step_consensus_capsule.go's splitConsensusImage so the
// round-trip (split → join) can be asserted without importing the steps package.
func splitLikeCapsule(full string) (repository, imageName string) {
	for i := len(full) - 1; i >= 0; i-- {
		if full[i] == '/' {
			return full[:i], full[i+1:]
		}
	}
	return "", full
}

func TestJoinImageRef_RoundTripsSplit(t *testing.T) {
	// A full registry/path/name must survive split-then-join unchanged, so the
	// reality read of an existing capsule feeds the same value back on re-run
	// (guards against the gcr.io/…/consensus-node → gcr.io decay).
	for _, full := range []string{
		"gcr.io/hedera-registry/consensus-node",
		"ghcr.io/hashgraph/solo-operator-uc",
		"registry.example.com:5000/team/app",
		"consensus-node",
	} {
		repo, name := splitLikeCapsule(full)
		if got := joinImageRef(repo, name); got != full {
			t.Errorf("round-trip of %q: got %q (repo=%q name=%q)", full, got, repo, name)
		}
	}
}

func TestJoinImageRef_Empties(t *testing.T) {
	if got := joinImageRef("", "consensus-node"); got != "consensus-node" {
		t.Errorf("empty repo: got %q", got)
	}
	if got := joinImageRef("gcr.io/x", ""); got != "gcr.io/x" {
		t.Errorf("empty name: got %q", got)
	}
	if got := joinImageRef("", ""); got != "" {
		t.Errorf("both empty: got %q", got)
	}
}
