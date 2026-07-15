// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"crypto/sha256"
	"os"

	"github.com/joomcode/errorx"
)

// RenderWeaverNft loads all policies from registryDir, renders the full
// `inet weaver` nft document, and atomically writes it to weaverNftPath
// (mode 0644). If the on-disk content is already identical (SHA-256 match)
// the write is skipped — making it safe to call from idempotent install flows.
//
// podCIDR is required only when at least one registered policy is a --stamp
// policy. If the caller passes "" and the existing weaverNftPath is readable,
// the pod CIDR is recovered from that file automatically — the same recovery
// path Manager.Create uses. An error is returned only when a stamp policy is
// present and no podCIDR can be resolved.
func RenderWeaverNft(registryDir, weaverNftPath, podCIDR string) error {
	policies, err := loadAll(registryDir)
	if err != nil {
		return err
	}
	if len(policies) == 0 {
		// An empty registry means no policies to enforce. A rendered empty chain
		// would be `policy drop` with no accept rule for new connections —
		// blackholing all forwarded traffic (pod startup, image pulls, inter-pod
		// DNS). Remove any stale persisted file so the boot oneshot's `test -e`
		// guard skips it and never replays a harmful or out-of-date inet weaver
		// table. "Empty registry" thus means "no file", not "an empty table".
		if err := os.Remove(weaverNftPath); err != nil && !os.IsNotExist(err) {
			return errorx.ExternalError.Wrap(err, "failed to remove stale %s for an empty policy registry", weaverNftPath)
		}
		return nil
	}

	// Validate each registry entry before rendering — a corrupt or hand-edited
	// JSON would otherwise flow into Render and produce a cryptic internal error
	// instead of a clear "corrupt registry entry" message. Mirrors the
	// validation Manager.Create performs on sibling entries before each render.
	for _, p := range policies {
		if err := p.Validate(nil); err != nil {
			return errorx.IllegalFormat.Wrap(err, "corrupt policy registry entry %s", registryPath(registryDir, p.Name))
		}
	}

	if podCIDR == "" && needsPodCIDR(policies) {
		if existing, readErr := os.ReadFile(weaverNftPath); readErr == nil {
			podCIDR = ExtractPodCIDR(string(existing))
		}
	}

	doc, err := Render(policies, podCIDR)
	if err != nil {
		return errorx.Decorate(err, "failed to render %s", weaverNftPath)
	}

	if existing, readErr := os.ReadFile(weaverNftPath); readErr == nil {
		if sha256.Sum256([]byte(doc)) == sha256.Sum256(existing) {
			return nil
		}
	}
	return atomicWriteFile(weaverNftPath, doc, 0o644)
}
