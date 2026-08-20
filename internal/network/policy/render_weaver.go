// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"crypto/sha256"
	"os"

	"github.com/joomcode/errorx"
)

// RenderWeaverNft loads all policies from registryDir, renders the full
// `inet weaver-workload-policy` nft document, and atomically writes it to weaverNftPath
// (mode 0644). If the on-disk content is already identical (SHA-256 match)
// the write is skipped — making it safe to call from idempotent install flows.
//
// podCIDRs (mixed IPv4/IPv6) are required only when at least one registered
// policy is a --stamp policy. If the caller passes none and the existing
// weaverNftPath is readable, the pod CIDRs are recovered from that file
// automatically — the same recovery path Manager.Create uses. An error is
// returned only when a stamp policy is present and no podCIDR can be resolved.
func RenderWeaverNft(registryDir, weaverNftPath string, podCIDRs ...string) error {
	policies, err := loadAll(registryDir)
	if err != nil {
		return err
	}
	if len(policies) == 0 {
		// An empty registry means there is nothing to classify. Remove any stale
		// persisted file so the boot oneshot's `test -e` guard skips it and never
		// replays an out-of-date inet weaver-workload-policy table. "Empty
		// registry" thus means "no file", not "an empty table".
		//
		// The chain is `policy accept`, so an empty table would be inert rather
		// than harmful — this is hygiene (never replay stale classification), not
		// the blackhole guard it was when the chain still defaulted to drop.
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

	// Drop empty entries so a caller passing a single "" (the common
	// "recover from the existing file" invocation) still triggers recovery
	// rather than being treated as one explicit-but-empty pod CIDR.
	podCIDRs = nonEmptyStrings(podCIDRs)
	if len(podCIDRs) == 0 && needsPodCIDR(policies) {
		if existing, readErr := os.ReadFile(weaverNftPath); readErr == nil {
			podCIDRs = ExtractPodCIDRs(string(existing))
		}
	}

	// Rendered with no membership: this is the provisioning path, which has no
	// nft runner and so no view of what is live. The sets come up empty and the
	// daemon's next successful poll replaces every one of them wholesale, which
	// also rewrites this artifact with the membership carried through. A reboot
	// in the gap between a re-provision and that poll therefore starts with
	// empty sets — the pre-existing behaviour, not a regression.
	doc, err := Render(policies, nil, podCIDRs...)
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
