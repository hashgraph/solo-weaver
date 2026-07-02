// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
)

// Manager implements `network policy create` against the `inet weaver` table.
// create takes the shared apply lock, writes the policy's registry file,
// re-renders the full chain from the registry in tier order, applies it to the
// live kernel with `nft -f`, atomically rewrites network-weaver.nft, and ensures
// the shared boot oneshot is enabled.
//
// create is create-if-missing, mirroring internal/network/firewall: an
// existing policy is left untouched (warn, no-op) unless --force is passed,
// which replaces its config and membership from the given flags/--cidrs.
//
// The rendered chain always begins with `delete table; add table` (§8.3.1:
// membership is never part of it), so every Apply() destroys and recreates
// every policy's live set, not just the one being created. Create snapshots
// every policy's membership first and restores it afterward -- see
// snapshotMembership below.
type Manager struct {
	runner        Runner
	weaverNftPath string
	registryDir   string
	lockPath      string
	ensureService func(ctx context.Context) error
}

// Config customises a Manager. The zero value is not useful; prefer NewManager.
// Tests inject a fake Runner, temp paths, and a no-op service func so the
// package builds and runs on any platform.
type Config struct {
	Runner        Runner
	WeaverNftPath string
	RegistryDir   string
	LockPath      string
	EnsureService func(ctx context.Context) error
}

// NewManager returns a Manager wired to the live kernel and the production paths.
func NewManager() *Manager {
	return NewManagerWithConfig(Config{})
}

// NewManagerWithConfig returns a Manager, filling any unset Config field with
// its production default.
func NewManagerWithConfig(cfg Config) *Manager {
	m := &Manager{
		runner:        cfg.Runner,
		weaverNftPath: cfg.WeaverNftPath,
		registryDir:   cfg.RegistryDir,
		lockPath:      cfg.LockPath,
		ensureService: cfg.EnsureService,
	}
	if m.runner == nil {
		m.runner = NewExecRunner()
	}
	if m.weaverNftPath == "" {
		m.weaverNftPath = WeaverNftPath
	}
	if m.registryDir == "" {
		m.registryDir = RegistryDir
	}
	if m.lockPath == "" {
		m.lockPath = LockPath
	}
	if m.ensureService == nil {
		m.ensureService = defaultEnsureService
	}
	return m
}

// Create adds a policy, or replaces an existing one when force is set.
// create-if-missing: a policy that doesn't exist is always created. A policy
// that already exists is left untouched (returns false) unless force is
// true, in which case its config and membership are replaced (not merged)
// from p and cidrs. cidrs is set membership, applied to the live kernel
// only — never persisted (§8.3.1).
func (m *Manager) Create(ctx context.Context, p *Policy, cidrs []string, podCIDR string, force bool) (bool, error) {
	if err := p.Validate(cidrs); err != nil {
		return false, err
	}
	// No blanket podCIDR requirement here: Render (below) only requires it
	// when the merged policy set actually contains a --stamp policy -- a
	// --deny-only chain never references POD_CIDR. When the caller doesn't
	// supply one (as --deny never does), recover the value last used to
	// render network-weaver.nft -- it's a deployment-wide constant, not a
	// per-call argument, so an unrelated --deny create shouldn't need it
	// re-supplied just to correctly re-render an unchanged --stamp sibling.
	if podCIDR == "" {
		if existing, err := os.ReadFile(m.weaverNftPath); err == nil {
			podCIDR = ExtractPodCIDR(string(existing))
		}
	}

	var changed bool
	err := m.withLock(func() error {
		policies, err := loadAll(m.registryDir)
		if err != nil {
			return err
		}

		// Re-validate sibling registry entries before rendering: a corrupt or
		// hand-edited /etc/solo-provisioner/policies/*.json would otherwise flow
		// straight into Render and could emit invalid nft or wrong semantics.
		// The entry matching p.Name is skipped — it is replaced by the freshly
		// validated p, so a re-create can heal a corrupt file for the same name.
		for _, lp := range policies {
			if lp.Name == p.Name {
				continue
			}
			if err := lp.Validate(nil); err != nil {
				return errorx.IllegalFormat.Wrap(err, "corrupt policy registry entry %s", registryPath(m.registryDir, lp.Name))
			}
		}

		existing := findByName(policies, p.Name)
		target, newCIDRs := p, cidrs
		if existing != nil && !force {
			// nft tables don't survive a reboot, and the boot oneshot doesn't
			// reload network-weaver.nft yet (#780) -- so "the registry has
			// this policy" does not imply "the live table has it too".
			tableExists, err := m.runner.Exists(ctx)
			if err != nil {
				return err
			}
			if tableExists {
				logx.As().Warn().Str("policy", p.Name).Msg(
					"network policy already exists — supplied flags/cidrs were not applied; pass --force to replace")
				return nil
			}
			// The table is missing underneath an existing registry entry
			// (manual `nft delete table`, or a reboot before #780). Self-heal
			// by re-rendering, but without --force we must not apply the
			// caller's new flags/cidrs -- only restore what was already
			// registered. Membership itself can't be recovered this way: it
			// was never persisted, so it comes back empty until --force
			// re-seeds it or the daemon's poll loop catches up.
			if len(cidrs) > 0 {
				logx.As().Warn().Str("policy", p.Name).Msg(
					"network policy's live table was missing; restoring its already-registered config, not the new flags/cidrs just supplied — pass --force to apply those")
			}
			if err := existing.Validate(nil); err != nil {
				return errorx.IllegalFormat.Wrap(err, "corrupt policy registry entry %s", registryPath(m.registryDir, p.Name))
			}
			target, newCIDRs = existing, nil
		}

		if existing != nil {
			// Preserve the original creation timestamp across a config change so
			// the tier tiebreaker (§8.4.7) stays stable.
			target.CreatedAt = existing.CreatedAt
		} else if target.CreatedAt.IsZero() {
			target.CreatedAt = time.Now().UTC()
		}

		// Snapshot every policy's live membership BEFORE Apply(): the
		// rendered document always does `delete table; add table` (set
		// membership is never part of that document, §8.3.1), so applying it
		// destroys and recreates every set in the table, not just target's.
		// Anything not explicitly restored afterward is gone -- permanently,
		// for operator-curated policies the daemon doesn't reconcile.
		snapshot, err := m.snapshotMembership(ctx, policies)
		if err != nil {
			return err
		}

		// Render the prospective full chain BEFORE touching disk so a render or
		// kernel-apply failure leaves the registry untouched.
		merged := upsert(policies, target)
		doc, err := Render(merged, podCIDR)
		if err != nil {
			return err
		}
		if err := m.runner.Apply(ctx, doc); err != nil {
			return err
		}
		// The table is now live in the kernel, emptied of all membership.
		// Restore every sibling's snapshot as-is; target's membership is
		// replaced with exactly newCIDRs, not merged with what was live
		// before (force means "this is the new desired state"). Any failure
		// from here leaves the kernel ahead of disk; decorate so the caller
		// reads it as "re-run to reconcile" (create is idempotent) rather
		// than "nothing happened".
		for _, lp := range merged {
			if !lp.hasCIDRSet() {
				continue
			}
			elements := snapshot[lp.Name]
			if lp.Name == target.Name {
				elements = setElements(target, newCIDRs)
			}
			if len(elements) == 0 {
				continue
			}
			if err := m.runner.AddElements(ctx, lp.Name, elements); err != nil {
				return errorx.Decorate(err, "inet weaver chain applied to the kernel but restoring %q membership failed; re-run to reconcile", lp.Name)
			}
		}
		if err := writeEntry(m.registryDir, target); err != nil {
			return errorx.Decorate(err, "inet weaver chain applied to the kernel but persisting the policy registry failed; re-run to reconcile")
		}
		if err := atomicWriteFile(m.weaverNftPath, doc, 0o644); err != nil {
			return errorx.Decorate(err, "inet weaver chain applied to the kernel but persisting %s failed; re-run to reconcile", m.weaverNftPath)
		}
		if err := m.ensureService(ctx); err != nil {
			return errorx.Decorate(err, "inet weaver chain applied and persisted but enabling %s failed", NetworkNftService)
		}
		changed = true
		return nil
	})
	return changed, err
}

// snapshotMembership captures the live membership of every policy that
// carries a CIDR set, before the caller runs a destructive Apply() that would
// otherwise wipe it. A ListElements failure aborts immediately (returned to
// the caller) rather than silently proceeding with a partial snapshot into a
// destructive apply.
func (m *Manager) snapshotMembership(ctx context.Context, policies []*Policy) (map[string][]string, error) {
	snapshot := make(map[string][]string, len(policies))
	for _, lp := range policies {
		if !lp.hasCIDRSet() {
			continue
		}
		elements, err := m.runner.ListElements(ctx, lp.Name)
		if err != nil {
			return nil, errorx.Decorate(err, "failed to snapshot live membership for policy %q before re-render", lp.Name)
		}
		if len(elements) > 0 {
			snapshot[lp.Name] = elements
		}
	}
	return snapshot, nil
}

// findByName returns the policy with the given name, or nil.
func findByName(policies []*Policy, name string) *Policy {
	for _, p := range policies {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// upsert returns policies with p inserted or replaced by name, name-sorted for
// a deterministic render.
func upsert(policies []*Policy, p *Policy) []*Policy {
	out := make([]*Policy, 0, len(policies)+1)
	for _, existing := range policies {
		if existing.Name != p.Name {
			out = append(out, existing)
		}
	}
	out = append(out, p)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// withLock serialises a mutation behind the shared cross-command flock so a
// hand-run operator command and the daemon poll loop (#754) cannot interleave
// nft transactions on the shared network tables.
func (m *Manager) withLock(fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(m.lockPath), 0o755); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to create lock directory %s", filepath.Dir(m.lockPath))
	}
	f, err := os.OpenFile(m.lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return errorx.ExternalError.Wrap(err, "failed to open lock file %s", m.lockPath)
	}
	defer func() { _ = f.Close() }()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to acquire lock %s", m.lockPath)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	return fn()
}
