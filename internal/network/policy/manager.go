// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
)

// Manager implements `network policy create` against the `inet weaver-workload-policy` table.
// create takes the shared apply lock, writes the policy's registry file,
// re-renders the full chain from the registry in tier order, applies it to the
// live kernel with `nft -f`, atomically rewrites network-weaver-workload-policy.nft, and ensures
// the shared boot oneshot is enabled.
//
// create is create-if-missing, mirroring internal/network/firewall: an
// existing policy is left untouched (warn, no-op) unless --force is passed,
// which replaces its config and membership from the given flags/--cidrs.
//
// The rendered chain always begins with `delete table; add table`, so every
// Apply() destroys and recreates every policy's live set, not just the one being
// created. Create snapshots every policy's membership first and renders it back
// into the document, so the table comes up already populated rather than being
// refilled by a second pass -- see snapshotMembership and persistMembership
// below. That inline membership is also what makes the sets boot-persistent:
// the shared oneshot replays this same document ahead of the daemon.
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
// from p and cidrs. cidrs is set membership: it never enters the registry, but
// it is rendered into the .nft as set elements along with every sibling's live
// membership, so the re-applied table and the boot artifact agree.
func (m *Manager) Create(ctx context.Context, p *Policy, cidrs []string, podCIDRs []string, force bool) (bool, error) {
	if err := p.Validate(cidrs); err != nil {
		return false, err
	}
	// Reject an overlapping --cidrs list before taking the lock or touching the
	// kernel. These entries reach the live sets as inline `elements = { … }` in
	// the document rendered below, and an interval set refuses conflicting
	// intervals however they arrive -- so the whole `nft -f` would fail, taking
	// the table with it. Catching it here keeps the registry and the kernel
	// untouched. Checked once for both families -- containment across families
	// is impossible, so a mixed list needs no split.
	if err := rejectContainment(p.Name, cidrs, nil); err != nil {
		return false, err
	}
	// No blanket podCIDR requirement here: Render (below) only requires it
	// when the merged policy set actually contains a --stamp policy -- a
	// --deny-only chain never references POD_CIDR. When the caller doesn't
	// supply one (as --deny never does), recover the value(s) last used to
	// render network-weaver-workload-policy.nft -- it's a deployment-wide constant, not a
	// per-call argument, so an unrelated --deny create shouldn't need it
	// re-supplied just to correctly re-render an unchanged --stamp sibling.
	//
	// Drop empty entries first (e.g. a Cobra StringSlice from `--pod-cidr ""`)
	// so recovery still fires instead of treating "" as one explicit-but-empty
	// pod CIDR — same normalization as RenderWeaverNft.
	podCIDRs = nonEmptyStrings(podCIDRs)
	if len(podCIDRs) == 0 {
		if existing, err := os.ReadFile(m.weaverNftPath); err == nil {
			podCIDRs = ExtractPodCIDRs(string(existing))
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

		// Reject a specific --stamp policy that would share its
		// (Direction, Ports) group with another already-registered
		// specific policy -- checked up front, for both the
		// create-if-missing and --force paths.
		if err := checkNoOverlap(policies, p); err != nil {
			return err
		}

		existing := findByName(policies, p.Name)
		target, newCIDRs := p, cidrs
		if existing != nil && !force {
			// The live kernel table can be missing even though the registry
			// has this policy -- e.g. a manual `nft delete table`, the shared
			// nft oneshot disabled, or network-weaver-workload-policy.nft
			// absent -- so "the registry has this policy" does not imply "the
			// live table has it too".
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
			// registered. Membership comes back empty here: the live sets are
			// gone, so there is nothing to snapshot, and the artifact is
			// rewritten from that empty snapshot. It repopulates when --force
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
		// rendered document always does `delete table; add table`, so applying
		// it destroys and recreates every set in the table, not just target's.
		// Anything missing from the document's own elements is gone --
		// permanently, for operator-curated policies the daemon doesn't
		// reconcile -- so every sibling's snapshot must be carried into it.
		snapshot, err := m.snapshotMembership(ctx, policies)
		if err != nil {
			return err
		}

		// The post-apply membership, which is both what gets restored to the
		// kernel below and what the document is rendered with, so the artifact
		// and the live table agree the moment this returns. Every sibling keeps
		// its snapshot; target's is replaced with exactly newCIDRs (split by
		// family), not merged with what was live before -- force means "this is
		// the new desired state".
		merged := upsert(policies, target)
		desired := make(map[string][]string, len(snapshot)+2)
		for setName, elems := range snapshot {
			desired[setName] = elems
		}
		if target.hasCIDRSet() {
			targetV4, targetV6 := setElementsByFamily(target, newCIDRs)
			desired[target.Name] = targetV4
			desired[V6SetName(target.Name)] = targetV6
		}

		// Render the prospective full chain BEFORE touching disk so a render or
		// kernel-apply failure leaves the registry untouched.
		doc, err := Render(merged, desired, podCIDRs...)
		if err != nil {
			return err
		}
		// The document declares every set with its elements inline, so the
		// `delete table; add table` comes back up already populated -- there is
		// no separate restore pass, and therefore no window in which the table
		// is live with empty sets. Re-adding afterwards would be redundant at
		// best: on the compound and listener-port sets (plain, not `flags
		// interval`) nft rejects an element already present outright.
		if err := m.runner.Apply(ctx, doc); err != nil {
			return err
		}
		if err := writeEntry(m.registryDir, target); err != nil {
			return errorx.Decorate(err, "inet weaver-workload-policy chain applied to the kernel but persisting the policy registry failed; re-run to reconcile")
		}
		if err := atomicWriteFile(m.weaverNftPath, doc, 0o644); err != nil {
			return errorx.Decorate(err, "inet weaver-workload-policy chain applied to the kernel but persisting %s failed; re-run to reconcile", m.weaverNftPath)
		}
		if err := m.ensureService(ctx); err != nil {
			return errorx.Decorate(err, "inet weaver-workload-policy chain applied and persisted but enabling %s failed", NetworkNftService)
		}
		changed = true
		return nil
	})
	return changed, err
}

// snapshotMembership captures the live contents of every daemon-owned set,
// keyed by set name. It serves two callers: a destructive Apply() that would
// otherwise wipe them, and persistMembership, which renders them back into the
// artifact. A ListElements failure aborts immediately (returned to the caller)
// rather than silently proceeding with a partial snapshot into a destructive
// apply — or, worse, persisting a partial snapshot as if it were the full state.
func (m *Manager) snapshotMembership(ctx context.Context, policies []*Policy) (map[string][]string, error) {
	snapshot := make(map[string][]string, len(policies))
	for _, lp := range policies {
		for _, setName := range daemonSetNames(lp) {
			elements, err := m.runner.ListElements(ctx, setName)
			if err != nil {
				return nil, errorx.Decorate(err, "failed to snapshot live membership for set %q before re-render", setName)
			}
			if len(elements) > 0 {
				snapshot[setName] = elements
			}
		}
	}
	return snapshot, nil
}

// cidrSetNames returns the nft set names that hold a policy's CIDR membership:
// the IPv4 set (the bare policy name) and its IPv6 companion (@<name>6). A
// --from-entity world policy has no membership set and returns nil.
func cidrSetNames(p *Policy) []string {
	if !p.hasCIDRSet() {
		return nil
	}
	return []string{p.Name, V6SetName(p.Name)}
}

// daemonSetNames returns every nft set name a policy renders whose contents are
// runtime state rather than registry config: the CIDR membership sets, plus the
// `<name>_ports` set when it is daemon-managed. A static `--ports` set is
// excluded — its elements come from the registry entry, so a re-render
// reproduces them and there is nothing to snapshot or persist.
func daemonSetNames(p *Policy) []string {
	names := cidrSetNames(p)
	if p.ManagedPorts && len(p.Ports) == 0 {
		names = append(names, PortsSetName(p.Name))
	}
	return names
}

// persistMembership re-renders network-weaver-workload-policy.nft from the
// registry and the live contents of every daemon-owned set, so the artifact the
// boot oneshot replays carries the membership that is currently in the kernel.
// This is what makes the sets survive a reboot: the oneshot runs ahead of the
// daemon, so a quarantined peer is dropped from the first forwarded packet
// rather than from the first successful statusz poll.
//
// It performs NO locking — every caller is already inside withLock/withLockNB,
// which is also what stops it from racing an operator re-render.
//
// Rendering from live kernel state rather than from the caller's desired map is
// deliberate: it keeps the one write path correct for the element-op verbs
// (add/remove) that mutate a set without knowing its full contents, and it means
// the artifact can never claim membership the kernel does not actually have.
//
// **It never fails its caller.** By the time it runs, the kernel write it
// follows has already succeeded — the reconcile did its job, and boot-durability
// is a step on top of that. Propagating a failure here would fail the whole
// reconcile, which for the daemon means superviseResponsibility faults the
// statusz poll loop and retries forever: a read-only /etc or a full disk would
// turn a correct kernel state into a permanent fault loop, re-applying nft on
// every backoff. So a failure is logged at WARN with a greppable reason and
// swallowed. The membership is right in the kernel; only its survival across a
// reboot is degraded, and that is what the log line says.
func (m *Manager) persistMembership(ctx context.Context) error {
	if err := m.renderAndWriteArtifact(ctx); err != nil {
		logx.As().Warn().Err(err).
			Str("reason", "PolicyMembershipNotPersisted").
			Str("path", m.weaverNftPath).
			Msg("live nft sets updated but the artifact could not be rewritten — membership is correct in the kernel but will not survive a reboot until a later apply succeeds")
	}
	return nil
}

// renderAndWriteArtifact is persistMembership's fallible half, split out so the
// error can be logged in one place and so tests can assert on it directly.
func (m *Manager) renderAndWriteArtifact(ctx context.Context) error {
	policies, err := loadAll(m.registryDir)
	if err != nil {
		return err
	}
	if len(policies) == 0 {
		// No registry means no table and no artifact (see Delete's last-policy
		// teardown); there is nothing to persist into.
		return nil
	}

	membership, err := m.snapshotMembership(ctx, policies)
	if err != nil {
		return err
	}

	// A stamp policy needs the pod CIDR to render, and this layer is never handed
	// one — it has to be recovered. Read the artifact once and reuse it for the
	// unchanged-content check below.
	existing, readErr := os.ReadFile(m.weaverNftPath)
	var podCIDRs []string
	if readErr == nil {
		podCIDRs = ExtractPodCIDRs(string(existing))
	}
	// Fall back to the live table when the artifact is missing or unreadable.
	// Without this, an operator who deletes the .nft out from under a live table
	// makes every subsequent reconcile fail on "pod CIDR is required" — after the
	// kernel write has already landed, so the daemon retries forever and never
	// converges. `nft list table` prints the same `ip daddr <cidr>` form Render
	// emits, so the same extractor reads it.
	if len(podCIDRs) == 0 {
		if live, listErr := m.runner.List(ctx); listErr == nil {
			podCIDRs = ExtractPodCIDRs(live)
		}
	}

	doc, err := Render(policies, membership, podCIDRs...)
	if err != nil {
		// Name the missing artifact when that is what stopped the recovery —
		// otherwise the wrapped error reads as a pod-CIDR problem and sends the
		// operator looking in the wrong place.
		if readErr != nil {
			return errorx.Decorate(err, "re-rendering %s failed (it could not be read: %v, and the live table yielded no pod CIDR)",
				m.weaverNftPath, readErr)
		}
		return errorx.Decorate(err, "re-rendering %s failed", m.weaverNftPath)
	}
	if readErr == nil && sha256.Sum256([]byte(doc)) == sha256.Sum256(existing) {
		return nil
	}
	if err := atomicWriteFile(m.weaverNftPath, doc, 0o644); err != nil {
		return errorx.Decorate(err, "persisting %s failed", m.weaverNftPath)
	}
	return nil
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

// Add appends cidrs to the live set for a named policy. The chain itself is not
// re-rendered — the set is mutated directly with `nft add element` — but
// network-weaver-workload-policy.nft is rewritten afterwards so the addition
// survives a reboot rather than silently reverting on the next boot.
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
		// An incremental add has to be checked against what is already in the
		// sets, not just against itself: nft refuses the add when a live member
		// covers the new entry (or vice versa). Read both families' live
		// membership and reject before mutating either set, so a rejected add
		// never lands half of its entries.
		live, err := m.liveMembership(ctx, p)
		if err != nil {
			return err
		}
		if err := rejectContainment(name, cidrs, live); err != nil {
			return err
		}
		v4, v6 := setElementsByFamily(p, cidrs)
		if err := m.runner.AddElements(ctx, name, v4); err != nil {
			return err
		}
		if err := m.runner.AddElements(ctx, V6SetName(name), v6); err != nil {
			return err
		}
		return m.persistMembership(ctx)
	})
}

// liveMembership returns the policy's current membership across both its IPv4
// and IPv6 sets, as the kernel spells it. Used by Add to check a candidate list
// against what is already stored; the two families are returned as one slice
// because containment can never span families.
func (m *Manager) liveMembership(ctx context.Context, p *Policy) ([]string, error) {
	var live []string
	for _, setName := range cidrSetNames(p) {
		elements, err := m.runner.ListElements(ctx, setName)
		if err != nil {
			return nil, errorx.Decorate(err, "failed to read live membership for set %q", setName)
		}
		live = append(live, elements...)
	}
	return live, nil
}

// Remove deletes cidrs from the live set for a named policy. Like Add, the
// chain is not re-rendered, but the .nft is rewritten so the removal is not
// undone by the next boot replay.
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
		v4, v6 := setElementsByFamily(p, cidrs)
		// Check every entry against live membership before deleting any of it.
		// `nft delete element` fails with a bare "element does not exist" for
		// anything absent, and the transaction is atomic -- so one wrong entry in
		// a batch removes NONE of the others while reporting a message that names
		// neither the offending entry nor the policy. The covered case
		// (removing a /32 that a member /24 subsumes) is the most misleading of
		// all, since the policy does permit that address.
		live, err := m.liveMembership(ctx, p)
		if err != nil {
			return err
		}
		if err := rejectMissingMembers(name, append(append([]string{}, v4...), v6...), live); err != nil {
			return err
		}
		if err := m.runner.DeleteElements(ctx, name, v4); err != nil {
			return err
		}
		if err := m.runner.DeleteElements(ctx, V6SetName(name), v6); err != nil {
			return err
		}
		return m.persistMembership(ctx)
	})
}

// Set atomically replaces the live set for a named policy with cidrs in a
// single `flush set + add element` kernel transaction. An empty cidrs slice
// clears the set. Like Add/Remove, the chain is not re-rendered, but the .nft is
// rewritten so the replacement survives a reboot.
func (m *Manager) Set(ctx context.Context, name string, cidrs []string) error {
	return m.withLock(func() error {
		if err := m.applySet(ctx, name, cidrs, authoredMembership); err != nil {
			return err
		}
		return m.persistMembership(ctx)
	})
}

// membershipSource says who authored the membership being written, which decides
// what happens when it carries a containment conflict nft would refuse (see
// cidrset.go). The two callers have opposite sources of truth, so they need
// opposite handling -- this is the one knob that distinguishes them.
type membershipSource int

const (
	// authoredMembership is membership an operator typed (`network policy
	// create`/`add`/`set`). A conflict is REJECTED: the operator's granularity
	// is the only record of intent, so silently folding it would leave a later
	// `network policy remove --cidr <covered>` unanswerable.
	authoredMembership membershipSource = iota
	// derivedMembership is membership recomputed from upstream by the
	// traffic-shaper daemon (ApplyMembership / ApplySets). A conflict is PRUNED:
	// the set is fully replaced every tick and no element is ever individually
	// deleted, so there is no authored granularity to preserve, and rejecting
	// would wedge the reconcile and leave a restrict-policy set empty.
	derivedMembership
)

// applySet replaces the live set for one named policy with cidrs via a single
// `flush set + add element` transaction (runner.SetElements). It performs NO
// locking: callers must already hold the shared apply lock (withLock for the
// CLI `set` verb, withLockNB for the daemon's ApplyMembership batch), so both
// paths funnel through the identical kernel transaction and can never
// interleave with a concurrent operator apply.
func (m *Manager) applySet(ctx context.Context, name string, cidrs []string, src membershipSource) error {
	p, err := m.requirePolicyWithCIDRSet(name)
	if err != nil {
		return err
	}
	if err := p.validateCIDRs(cidrs); err != nil {
		return err
	}
	// A full replace only has to agree with itself -- whatever was live is about
	// to be flushed -- so the containment check is against cidrs alone. Which way
	// a conflict is handled depends on who authored the list; see
	// membershipSource.
	switch src {
	case authoredMembership:
		if err := rejectContainment(name, cidrs, nil); err != nil {
			return err
		}
	case derivedMembership:
		if kept := PruneContainedCIDRs(cidrs); len(kept) != len(cidrs) {
			// Steady state for a persistently redundant upstream, so this is
			// Debug rather than Warn: it would otherwise repeat every poll tick.
			logx.As().Debug().
				Str("policy", name).
				Int("supplied", len(cidrs)).
				Int("applied", len(kept)).
				Msg("dropped CIDRs already covered by another member of the same policy set")
			cidrs = kept
		}
	}
	if err := m.requireTableExists(ctx, name); err != nil {
		return err
	}
	// Full-replace each family's set. SetElements flushes then adds, so a family
	// with no members in cidrs is cleared (not left stale) — required for the
	// daemon's present/absent reconcile semantics to hold per family.
	v4, v6 := setElementsByFamily(p, cidrs)
	if err := m.runner.SetElements(ctx, name, v4); err != nil {
		return err
	}
	return m.runner.SetElements(ctx, V6SetName(name), v6)
}

// ApplyMembership pushes desired CIDR membership into one or more policies'
// live `inet weaver-workload-policy` sets. It is the traffic-shaper daemon's write path, and
// it reuses the same per-policy transaction as the hand-run `network policy
// set` CLI (applySet -> runner.SetElements), so both produce identical kernel
// state for the same input.
//
// Each map entry is a full-list replace (like `set`): an empty slice clears
// that policy's set. Policies are applied in deterministic (sorted) name
// order, one `nft -f` transaction per policy for atomicity.
//
// The whole batch runs under a SINGLE non-blocking acquisition of the shared
// apply flock. The return value is:
//   - (false, nil): the lock was already held by a hand-run operator command,
//     so nothing was written and the caller skips this tick rather than
//     blocking or interleaving nft transactions;
//   - (true, nil):  the lock was acquired and every policy applied cleanly;
//   - (false, err): the lock was acquired but a policy failed mid-batch — the
//     batch is not considered applied (see the partial-commit note below).
//
// ApplyMembership never calls `create`: a name absent from the registry, or
// one carrying no CIDR set, is an error (the policy structure is fixed by
// `block node install`). An error mid-batch stops the batch — policies applied
// before it stay committed and the caller re-drives on the next tick, which is
// safe because the apply is idempotent.
//
// It acquires the lock itself, so it must NOT be called while the caller
// already holds withLock/withLockNB (doing so would self-deadlock on a second
// open-file-description of the same lock file).
func (m *Manager) ApplyMembership(ctx context.Context, desired map[string][]string) (applied bool, err error) {
	if len(desired) == 0 {
		return true, nil
	}
	names := make([]string, 0, len(desired))
	for name := range desired {
		names = append(names, name)
	}
	sort.Strings(names)

	acquired, err := m.withLockNB(func() error {
		for _, name := range names {
			if err := m.applySet(ctx, name, desired[name], derivedMembership); err != nil {
				return err
			}
		}
		return m.persistMembership(ctx)
	})
	if err != nil {
		return false, err
	}
	return acquired, nil
}

// ApplySets pushes desired CIDR membership AND listener ports to the live nft
// sets under a SINGLE non-blocking acquisition of the shared apply flock, so a
// reconcile tick is atomic: either both dimensions are written, or the whole tick
// is skipped because an operator command holds the lock. It is the traffic-shaper
// daemon's write path. Applying the two dimensions under two separate
// acquisitions (ApplyMembership then ApplyPorts) would open a partial-apply
// window where an operator could take the lock between them and leave one
// dimension stale until the next force-resync.
//
// membership maps a policy name to its desired CIDR set; ports maps a
// managed-ports policy name to its desired `<name>_ports` set. Each entry is a
// full-list replace (an empty slice clears that set). Membership sets are applied
// first, then port sets, each one `nft -f` transaction, in sorted name order. The
// return contract matches ApplyMembership:
//   - (false, nil): the lock was held by a hand-run operator command, nothing was
//     written, the caller skips this tick;
//   - (true, nil):  the lock was acquired and both dimensions applied cleanly;
//   - (false, err): the lock was acquired but a set failed mid-batch.
//
// It acquires the lock itself, so it must NOT be called while the caller already
// holds withLock/withLockNB.
// Empty maps are NOT a no-op: the lock is still taken and the artifact still
// re-persisted. A converged node produces no deltas tick after tick, so an early
// return here would mean the artifact is only ever written when membership
// changes — leaving a node that was already converged when this build landed
// with a permanently stale artifact, and no way to notice.
func (m *Manager) ApplySets(ctx context.Context, membership, ports map[string][]string) (applied bool, err error) {
	memNames := sortedMapKeys(membership)
	portNames := sortedMapKeys(ports)

	acquired, err := m.withLockNB(func() error {
		for _, name := range memNames {
			if err := m.applySet(ctx, name, membership[name], derivedMembership); err != nil {
				return err
			}
		}
		for _, name := range portNames {
			if err := m.applyPortsSet(ctx, name, ports[name]); err != nil {
				return err
			}
		}
		// Inside the same acquisition as the kernel writes: the artifact is
		// rendered from live state, so persisting under a different lock hold
		// could capture a set an operator changed in between.
		return m.persistMembership(ctx)
	})
	if err != nil {
		return false, err
	}
	return acquired, nil
}

// sortedMapKeys returns m's keys in sorted order, for deterministic apply order.
func sortedMapKeys(m map[string][]string) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ApplyPorts pushes desired listener ports into one or more managed-ports
// policies' live `<name>_ports` sets. It is the traffic-shaper daemon's
// listener-port write path — the exact counterpart of ApplyMembership for the
// CIDR sets — reconciling each set from the BN's statusz local.port. It shares
// ApplyMembership's batching, single non-blocking acquisition of the shared
// apply flock, and return contract:
//   - (false, nil): the lock was already held by a hand-run operator command, so
//     nothing was written and the caller skips this tick;
//   - (true, nil):  the lock was acquired and every policy applied cleanly;
//   - (false, err): the lock was acquired but a policy failed mid-batch.
//
// Each map entry is a full-list replace: an empty slice clears that policy's
// ports set. A name absent from the registry, or one whose ports are static
// rather than daemon-managed, is an error (the policy structure is fixed by
// `block node install`). Like ApplyMembership it acquires the lock itself, so it
// must NOT be called while the caller already holds withLock/withLockNB.
func (m *Manager) ApplyPorts(ctx context.Context, desired map[string][]string) (applied bool, err error) {
	if len(desired) == 0 {
		return true, nil
	}
	names := make([]string, 0, len(desired))
	for name := range desired {
		names = append(names, name)
	}
	sort.Strings(names)

	acquired, err := m.withLockNB(func() error {
		for _, name := range names {
			if err := m.applyPortsSet(ctx, name, desired[name]); err != nil {
				return err
			}
		}
		return m.persistMembership(ctx)
	})
	if err != nil {
		return false, err
	}
	return acquired, nil
}

// applyPortsSet replaces the live `<name>_ports` set for one managed-ports
// policy with ports via a single `flush set + add element` transaction. Like
// applySet (its CIDR-membership sibling) it performs NO locking: callers must
// already hold the shared apply lock. Every port is validated before the write
// so a malformed statusz value can never poison the nft transaction.
func (m *Manager) applyPortsSet(ctx context.Context, name string, ports []string) error {
	p, err := m.requirePolicyWithPortsSet(name)
	if err != nil {
		return err
	}
	for _, port := range ports {
		if err := sanity.ValidatePort(port); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid listener port %q for policy %q", port, name)
		}
	}
	if err := m.requireTableExists(ctx, name); err != nil {
		return err
	}
	return m.runner.SetElements(ctx, PortsSetName(p.Name), ports)
}

// requirePolicyWithPortsSet loads the named policy and verifies it carries a
// daemon-managed ports set (ManagedPorts). Returns an error if the policy is
// missing or its ports are static/none — the daemon must only ever write the
// managed-ports sets `block node install` declared, never a static or absent one.
func (m *Manager) requirePolicyWithPortsSet(name string) (*Policy, error) {
	p, err := readEntry(m.registryDir, name)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, errorx.IllegalState.New(
			"policy %q not found; run `network policy create` with --name and the original policy flags first", name)
	}
	if !p.ManagedPorts {
		return nil, errorx.IllegalArgument.New(
			"policy %q has no daemon-managed ports set; its listener ports are static, not reconciled from statusz", name)
	}
	return p, nil
}

// Show returns a human-readable summary of a named policy: its registry config
// (action, class, ports, created_at) followed by the live set membership from
// the kernel (`nft list set inet weaver-workload-policy <name>`). No lock is taken — Show is
// read-only.
func (m *Manager) Show(ctx context.Context, name string) (string, error) {
	p, err := readEntry(m.registryDir, name)
	if err != nil {
		return "", err
	}
	if p == nil {
		return "", errorx.IllegalState.New("policy %q not found", name)
	}
	return m.showOne(ctx, p)
}

// ShowAll returns a summary of every configured policy, sorted by name, in the
// same format Show produces for a single policy. With no policies configured it
// returns a friendly message rather than an error, mirroring `network shape
// show`. No lock is taken — it is read-only.
func (m *Manager) ShowAll(ctx context.Context) (string, error) {
	policies, err := loadAll(m.registryDir)
	if err != nil {
		return "", err
	}
	if len(policies) == 0 {
		return "no policies configured\n", nil
	}
	var b strings.Builder
	for i, p := range policies {
		out, err := m.showOne(ctx, p)
		if err != nil {
			return "", err
		}
		if i > 0 {
			b.WriteString("\n") // blank line between policies
		}
		b.WriteString(out)
	}
	return b.String(), nil
}

// showOne renders a single policy's registry config (action, class, ports,
// created_at) followed by its live set membership from the kernel. No lock is
// taken — it is read-only.
func (m *Manager) showOne(ctx context.Context, p *Policy) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "policy: %s\n", p.Name)
	// Direction is the most operationally significant attribute, so it leads.
	if p.Direction != "" {
		fmt.Fprintf(&b, "  direction: %s\n", p.Direction)
	}
	fmt.Fprintf(&b, "  action:  %s\n", p.Action)
	if p.Stamp != "" {
		fmt.Fprintf(&b, "  class:   %s\n", p.Stamp)
	}
	if p.ReplyStamp != "" {
		fmt.Fprintf(&b, "  reply-class: %s\n", p.ReplyStamp)
	}
	if len(p.Ports) > 0 {
		fmt.Fprintf(&b, "  ports:   %s\n", strings.Join(p.Ports, ", "))
	}
	if p.ManagedPorts {
		ports, err := m.runner.ListElements(ctx, PortsSetName(p.Name))
		if err != nil {
			return "", err
		}
		if len(ports) == 0 {
			b.WriteString("  ports:   managed (reconciled from statusz; none yet)\n")
		} else {
			fmt.Fprintf(&b, "  ports:   managed (reconciled from statusz): %s\n", strings.Join(ports, ", "))
		}
	}
	if p.FromEntityWorld {
		b.WriteString("  from-entity: world\n")
	}
	fmt.Fprintf(&b, "  created: %s\n", p.CreatedAt.Format(time.RFC3339))

	if !p.hasCIDRSet() {
		b.WriteString("  live set: none (--from-entity world policy; any source/dest matches, no IP-set)\n")
		return b.String(), nil
	}

	// Show both families' live sets (@<name> and @<name>6).
	for _, setName := range cidrSetNames(p) {
		elements, err := m.runner.ListElements(ctx, setName)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "  live set @%s:\n", setName)
		if len(elements) == 0 {
			b.WriteString("    (empty)\n")
		} else {
			for _, e := range elements {
				fmt.Fprintf(&b, "    %s\n", e)
			}
		}
	}
	return b.String(), nil
}

// Delete removes a named policy: re-renders the `inet weaver-workload-policy` chain without
// it, applies the result to the live kernel, restores the remaining policies'
// live membership (which the destructive re-render wipes), removes the
// registry file, and atomically rewrites network-weaver-workload-policy.nft.
//
// If this is the last policy, the table is torn down entirely — live and on
// disk — rather than left registered with nothing to classify. The boot oneshot
// is left enabled.
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

		if len(remaining) == 0 {
			// Deleting the last policy: tear the whole table down rather than
			// render a chain with no rules, which Apply() would load into the
			// kernel and atomicWrite would persist for replay at boot. Remove the
			// live table (if present) and the persisted file so an empty registry
			// means "no inet weaver-workload-policy table", live or on disk.
			if exists, err := m.runner.Exists(ctx); err != nil {
				return errorx.Decorate(err, "failed to check the inet weaver-workload-policy table while removing the last policy")
			} else if exists {
				if err := m.runner.Delete(ctx); err != nil {
					return errorx.Decorate(err, "failed to delete the inet weaver-workload-policy table while removing the last policy")
				}
			}
			if err := os.Remove(m.weaverNftPath); err != nil && !os.IsNotExist(err) {
				return errorx.Decorate(errorx.ExternalError.Wrap(err, "failed to remove %s", m.weaverNftPath),
					"inet weaver-workload-policy table deleted but removing the persisted file failed; re-run to reconcile")
			}
			if err := os.Remove(registryPath(m.registryDir, name)); err != nil && !os.IsNotExist(err) {
				return errorx.Decorate(errorx.ExternalError.Wrap(err, "failed to remove registry file for %q", name),
					"inet weaver-workload-policy table and file removed but removing the registry entry failed; re-run to reconcile")
			}
			logx.As().Info().Str("policy", name).Msg("network policy deleted (last policy — inet weaver-workload-policy table torn down)")
			return nil
		}

		// Recover the pod CIDR(s) from the existing .nft if any remaining policy
		// is a --stamp (same pattern as Create).
		var podCIDRs []string
		if needsPodCIDR(remaining) {
			if existing, err := os.ReadFile(m.weaverNftPath); err == nil {
				podCIDRs = ExtractPodCIDRs(string(existing))
			}
		}

		// Snapshot remaining policies' membership BEFORE Apply(): the rendered
		// document always does `delete table; add table`, which wipes every set
		// in the table, not just the deleted policy's.
		snapshot, err := m.snapshotMembership(ctx, remaining)
		if err != nil {
			return err
		}

		// Rendering with the snapshot means the re-applied table comes back up
		// already populated, so the remaining policies' sets are never live and
		// empty, and no separate restore pass is needed.
		doc, err := Render(remaining, snapshot, podCIDRs...)
		if err != nil {
			return err
		}
		if err := m.runner.Apply(ctx, doc); err != nil {
			return err
		}

		// Write the .nft file before removing the registry so that a failed
		// write leaves the registry intact and a re-run can find the policy.
		if err := atomicWriteFile(m.weaverNftPath, doc, 0o644); err != nil {
			return errorx.Decorate(err,
				"inet weaver-workload-policy chain re-rendered but persisting %s failed; re-run to reconcile", m.weaverNftPath)
		}
		if err := os.Remove(registryPath(m.registryDir, name)); err != nil && !os.IsNotExist(err) {
			return errorx.Decorate(
				errorx.ExternalError.Wrap(err, "failed to remove registry file for %q", name),
				"inet weaver-workload-policy chain persisted but removing the registry file failed; re-run to reconcile",
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

// requireTableExists returns a clear error when the inet weaver-workload-policy table is
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

// openLockFile creates the lock directory if needed and opens the shared
// apply-lock file. The caller owns the returned handle and must Close it (which
// also releases any flock held on it).
func (m *Manager) openLockFile() (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(m.lockPath), 0o755); err != nil {
		return nil, errorx.ExternalError.Wrap(err, "failed to create lock directory %s", filepath.Dir(m.lockPath))
	}
	f, err := os.OpenFile(m.lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, errorx.ExternalError.Wrap(err, "failed to open lock file %s", m.lockPath)
	}
	return f, nil
}

// withLock serialises a mutation behind the shared cross-command flock so a
// hand-run operator command and the daemon poll loop cannot interleave
// nft transactions on the shared network tables. It blocks until the lock is
// available (LOCK_EX).
func (m *Manager) withLock(fn func() error) error {
	f, err := m.openLockFile()
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to acquire lock %s", m.lockPath)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	return fn()
}

// withLockNB is the non-blocking counterpart to withLock (LOCK_EX|LOCK_NB). It
// is the daemon poll loop's acquisition mode: when a hand-run operator command
// is mid-apply and already holds the lock, withLockNB does NOT run fn and
// returns (false, nil) so the caller skips this tick, rather than blocking and
// risking interleaved nft transactions. On a clean acquisition it runs fn and
// returns (true, fn()'s error). Any lock error other than "would block" is
// returned with acquired=false.
func (m *Manager) withLockNB(fn func() error) (acquired bool, err error) {
	f, err := m.openLockFile()
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		// A held lock surfaces as EWOULDBLOCK, which is the same errno value as
		// EAGAIN on the platforms this runs on (Linux daemon, darwin dev), so
		// errors.Is here also matches the EAGAIN spelling — the tick is skipped
		// cleanly rather than being reported as a lock failure.
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return false, nil
		}
		return false, errorx.ExternalError.Wrap(err, "failed to acquire lock %s", m.lockPath)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	return true, fn()
}
