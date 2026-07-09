// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
// The rendered chain always begins with `delete table; add table` (membership
// is never part of it), so every Apply() destroys and recreates every policy's
// live set, not just the one being created. Create snapshots every policy's
// membership first and restores it afterward -- see snapshotMembership below.
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
// only — never persisted.
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
			// reload network-weaver.nft yet -- so "the registry has this
			// policy" does not imply "the live table has it too".
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
			// (manual `nft delete table`, or a reboot). Self-heal
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
			// the tier tiebreaker stays stable.
			target.CreatedAt = existing.CreatedAt
		} else if target.CreatedAt.IsZero() {
			target.CreatedAt = time.Now().UTC()
		}

		// Snapshot every policy's live membership BEFORE Apply(): the
		// rendered document always does `delete table; add table` (set
		// membership is never part of that document), so applying it
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

// Add appends cidrs to the live set for a named policy. The set is mutated
// directly with `nft add element` — no chain re-render occurs, so
// network-weaver.nft is not updated (membership is never persisted).
// Returns an error if the policy does not exist, has no CIDR set
// (--from-entity world), or the live kernel table is not present.
func (m *Manager) Add(ctx context.Context, name string, cidrs []string) error {
	if len(cidrs) == 0 {
		return errorx.IllegalArgument.New("at least one --cidr is required")
	}
	return m.withLock(func() error {
		p, err := m.requirePolicyWithCIDRSet(name)
		if err != nil {
			return err
		}
		if err := p.validateCIDRs(cidrs); err != nil {
			return err
		}
		if err := m.requireTableExists(ctx, name); err != nil {
			return err
		}
		return m.runner.AddElements(ctx, name, setElements(p, cidrs))
	})
}

// Remove deletes cidrs from the live set for a named policy. Like Add, only
// the live kernel set is changed — no chain re-render and no .nft update.
func (m *Manager) Remove(ctx context.Context, name string, cidrs []string) error {
	if len(cidrs) == 0 {
		return errorx.IllegalArgument.New("at least one --cidr is required")
	}
	return m.withLock(func() error {
		p, err := m.requirePolicyWithCIDRSet(name)
		if err != nil {
			return err
		}
		if err := p.validateCIDRs(cidrs); err != nil {
			return err
		}
		if err := m.requireTableExists(ctx, name); err != nil {
			return err
		}
		return m.runner.DeleteElements(ctx, name, setElements(p, cidrs))
	})
}

// Set atomically replaces the live set for a named policy with cidrs in a
// single `flush set + add element` kernel transaction. An empty cidrs slice
// clears the set. Like Add/Remove, only the live kernel set is changed.
func (m *Manager) Set(ctx context.Context, name string, cidrs []string) error {
	return m.withLock(func() error {
		p, err := m.requirePolicyWithCIDRSet(name)
		if err != nil {
			return err
		}
		if err := p.validateCIDRs(cidrs); err != nil {
			return err
		}
		if err := m.requireTableExists(ctx, name); err != nil {
			return err
		}
		return m.runner.SetElements(ctx, name, setElements(p, cidrs))
	})
}

// Show returns a human-readable summary of a named policy: its registry config
// (action, class, ports, created_at) followed by the live set membership from
// the kernel (`nft list set inet weaver <name>`). No lock is taken — Show is
// read-only.
func (m *Manager) Show(ctx context.Context, name string) (string, error) {
	p, err := readEntry(m.registryDir, name)
	if err != nil {
		return "", err
	}
	if p == nil {
		return "", errorx.IllegalState.New("policy %q not found", name)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "policy: %s\n", p.Name)
	fmt.Fprintf(&b, "  action:  %s\n", p.Action)
	if p.Stamp != "" {
		fmt.Fprintf(&b, "  class:   %s\n", p.Stamp)
	}
	if p.ReplyStamp != "" {
		fmt.Fprintf(&b, "  reply-class: %s\n", p.ReplyStamp)
	}
	if p.Direction != "" {
		fmt.Fprintf(&b, "  direction: %s\n", p.Direction)
	}
	if len(p.Ports) > 0 {
		fmt.Fprintf(&b, "  ports:   %s\n", strings.Join(p.Ports, ", "))
	}
	if p.FromEntityWorld {
		b.WriteString("  from-entity: world\n")
	}
	fmt.Fprintf(&b, "  created: %s\n", p.CreatedAt.Format(time.RFC3339))

	if !p.hasCIDRSet() {
		b.WriteString("\nlive set: none (--from-entity world policy; any source/dest matches, no IP-set)\n")
		return b.String(), nil
	}

	elements, err := m.runner.ListElements(ctx, name)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&b, "\nlive set @%s:\n", name)
	if len(elements) == 0 {
		b.WriteString("  (empty)\n")
	} else {
		for _, e := range elements {
			fmt.Fprintf(&b, "  %s\n", e)
		}
	}
	return b.String(), nil
}

// Delete removes a named policy: re-renders the `inet weaver` chain without
// it, applies the result to the live kernel, restores the remaining policies'
// live membership (which the destructive re-render wipes), removes the
// registry file, and atomically rewrites network-weaver.nft.
//
// If this is the last policy, an empty chain (policy drop, no rules) is
// applied and the boot oneshot is left enabled.
func (m *Manager) Delete(ctx context.Context, name string) error {
	return m.withLock(func() error {
		policies, err := loadAll(m.registryDir)
		if err != nil {
			return err
		}
		if findByName(policies, name) == nil {
			return errorx.IllegalState.New("policy %q not found", name)
		}

		remaining := make([]*Policy, 0, len(policies)-1)
		for _, p := range policies {
			if p.Name != name {
				remaining = append(remaining, p)
			}
		}

		// Re-validate sibling entries before rendering.
		for _, lp := range remaining {
			if err := lp.Validate(nil); err != nil {
				return errorx.IllegalFormat.Wrap(err, "corrupt policy registry entry %s", registryPath(m.registryDir, lp.Name))
			}
		}

		// Recover the pod CIDR from the existing .nft if any remaining policy
		// is a --stamp (same pattern as Create).
		podCIDR := ""
		if needsPodCIDR(remaining) {
			if existing, err := os.ReadFile(m.weaverNftPath); err == nil {
				podCIDR = ExtractPodCIDR(string(existing))
			}
		}

		// Snapshot remaining policies' membership BEFORE Apply(): the rendered
		// document always does `delete table; add table`, which wipes every set
		// in the table, not just the deleted policy's.
		snapshot, err := m.snapshotMembership(ctx, remaining)
		if err != nil {
			return err
		}

		doc, err := Render(remaining, podCIDR)
		if err != nil {
			return err
		}
		if err := m.runner.Apply(ctx, doc); err != nil {
			return err
		}

		// Restore remaining policies' membership.
		for _, lp := range remaining {
			if !lp.hasCIDRSet() {
				continue
			}
			elements := snapshot[lp.Name]
			if len(elements) == 0 {
				continue
			}
			if err := m.runner.AddElements(ctx, lp.Name, elements); err != nil {
				return errorx.Decorate(err,
					"inet weaver chain re-rendered but restoring %q membership failed; re-run to reconcile", lp.Name)
			}
		}

		// Write the .nft file before removing the registry so that a failed
		// write leaves the registry intact and a re-run can find the policy.
		if err := atomicWriteFile(m.weaverNftPath, doc, 0o644); err != nil {
			return errorx.Decorate(err,
				"inet weaver chain re-rendered but persisting %s failed; re-run to reconcile", m.weaverNftPath)
		}
		if err := os.Remove(registryPath(m.registryDir, name)); err != nil && !os.IsNotExist(err) {
			return errorx.Decorate(
				errorx.ExternalError.Wrap(err, "failed to remove registry file for %q", name),
				"inet weaver chain persisted but removing the registry file failed; re-run to reconcile",
			)
		}
		logx.As().Info().Str("policy", name).Msg("network policy deleted")
		return nil
	})
}

// requirePolicyWithCIDRSet loads the named policy from the registry and
// verifies it has a CIDR set (@<name>). Returns an error if the policy is
// missing or uses --from-entity world (those policies match any source/dest
// and render no IP-set, so element verbs do not apply).
func (m *Manager) requirePolicyWithCIDRSet(name string) (*Policy, error) {
	p, err := readEntry(m.registryDir, name)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, errorx.IllegalState.New(
			"policy %q not found; run `network policy create` with --name and the original policy flags first", name)
	}
	if !p.hasCIDRSet() {
		return nil, errorx.IllegalArgument.New(
			"policy %q has no CIDR set (it uses --from-entity world); element verbs do not apply", name)
	}
	return p, nil
}

// requireTableExists returns a clear error when the inet weaver table is
// absent from the kernel, so element verbs (add/remove/set) surface a helpful
// message instead of propagating the raw nft "No such file" error.
func (m *Manager) requireTableExists(ctx context.Context, name string) error {
	exists, err := m.runner.Exists(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return errorx.IllegalState.New(
			"policy table not found; run `network policy create` with --name and the original policy flags to restore")
	}
	return nil
}

// withLock serialises a mutation behind the shared cross-command flock so a
// hand-run operator command and the daemon poll loop cannot interleave
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
