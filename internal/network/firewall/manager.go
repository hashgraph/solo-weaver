// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
)

// Manager implements the `network firewall` verbs against the `inet weaver-host-firewall`
// table. Every mutating verb takes the shared apply lock, atomically rewrites
// the on-disk artifact, and then restarts the systemd service via DBus so the
// kernel is updated in one consistent operation — no separate nft apply exec.
type Manager struct {
	runner     Runner
	nftPath    string
	configPath string
	// prevConfigPath holds the generation of configPath immediately before the
	// current one. Derived from configPath rather than injected, so a Manager
	// wired to temp paths in a test retains alongside its own config without
	// having to know the convention.
	prevConfigPath  string
	lockPath        string
	applyViaService func(ctx context.Context) error
}

// Config customises a Manager. The zero value is not useful; prefer NewManager.
// Tests inject a fake Runner, temp paths, and a no-op service func so the
// package builds and runs on any platform.
type Config struct {
	Runner          Runner
	NftPath         string
	ConfigPath      string
	LockPath        string
	ApplyViaService func(ctx context.Context) error
}

// NewManager returns a Manager wired to the live kernel and the production
// paths.
func NewManager() *Manager {
	return NewManagerWithConfig(Config{})
}

// NewManagerWithConfig returns a Manager, filling any unset Config field with
// its production default.
func NewManagerWithConfig(cfg Config) *Manager {
	m := &Manager{
		runner:          cfg.Runner,
		nftPath:         cfg.NftPath,
		configPath:      cfg.ConfigPath,
		lockPath:        cfg.LockPath,
		applyViaService: cfg.ApplyViaService,
	}
	if m.runner == nil {
		m.runner = NewExecRunner()
	}
	if m.nftPath == "" {
		m.nftPath = HostNftPath
	}
	if m.configPath == "" {
		m.configPath = HostConfigPath
	}
	m.prevConfigPath = m.configPath + HostConfigPrevSuffix
	if m.lockPath == "" {
		m.lockPath = LockPath
	}
	if m.applyViaService == nil {
		m.applyViaService = defaultApplyViaService
	}
	return m
}

// Create is create-if-missing: when the table already exists and force is
// false, it makes no changes and returns (false, nil). force re-renders the
// table from the supplied flags and returns (true, nil).
func (m *Manager) Create(ctx context.Context, t *Table, force bool) (bool, error) {
	if err := t.Validate(); err != nil {
		return false, err
	}
	var changed bool
	err := m.withLock(func() error {
		exists, err := m.runner.Exists(ctx)
		if err != nil {
			return err
		}
		if exists && !force {
			logx.As().Warn().Str("table", TableName).Msg("inet weaver-host-firewall firewall already exists — supplied flags were not applied; pass --force to re-render from the current flags")
			return nil
		}
		changed = true
		return m.applyAndPersist(ctx, t)
	})
	return changed, err
}

// Apply replaces the whole table from a declarative config and re-renders,
// regardless of whether one already exists: unlike Create it is not
// create-if-missing. It has no production caller today (`create --from-file`
// goes through Create) and is retained as the exported "replace outright"
// entry point, exercised by the package tests.
func (m *Manager) Apply(ctx context.Context, t *Table) error {
	if err := t.Validate(); err != nil {
		return err
	}
	return m.withLock(func() error { return m.applyAndPersist(ctx, t) })
}

// CreateRule declares a named allow rule, so a rule can be brought into
// existence without a config file. It is create-if-missing like Create: a name
// that already exists is left alone and reported as unchanged unless force is
// set, in which case the rule is replaced outright — the declaration states the
// whole rule, so every field not supplied on the redeclare returns to its
// default, membership and matching alike.
//
// The rule may be declared with no members; the element verbs populate it
// afterwards. It renders nothing until it has both sources and a destination, so
// running the declare and the populate as separate commands never opens access
// early.
//
// Deliberately not built on mutate: mutate always re-applies, and the
// already-exists path has nothing to apply — re-rendering an identical document
// would restart the nft unit for no reason.
func (m *Manager) CreateRule(ctx context.Context, r Rule, force bool) (bool, error) {
	// Ahead of the exists check below, because the reserved blocks always exist:
	// left to that branch, `create-allow-rule --name mgmt` would report "already
	// exists" and succeed, when what the operator needs to be told is that mgmt
	// is not an allow rule at all.
	if IsReserved(r.Name) {
		return false, errorx.IllegalArgument.New(
			"%q is a reserved name and cannot be used for an allow rule; configure it under its own %q block instead", r.Name, r.Name)
	}

	var changed bool
	err := m.withLock(func() error {
		t, err := m.load()
		if err != nil {
			return err
		}
		if _, exists := t.Rule(r.Name); exists && !force {
			logx.As().Warn().Str("rule", r.Name).Msg(
				"allow rule already exists — the supplied flags were not applied; pass --force to replace it, which resets the whole rule (addresses, ports, proto and icmp_echo)")
			return nil
		}
		// UpsertAllow rejects the reserved names and runs Rule.Validate, so a
		// CLI-declared rule is held to exactly the same rules as a file-declared
		// one. Table.Validate then catches set-name collisions before render.
		if err := t.UpsertAllow(r); err != nil {
			return err
		}
		changed = true
		return m.applyAndPersist(ctx, t)
	})
	return changed, err
}

// Add adds CIDRs and/or port specs to the named rule and re-renders. Adding is
// idempotent: an entry already present is left alone. Growing a rule cannot
// empty the management allowlist, so unlike Remove/Set it takes no force.
func (m *Manager) Add(ctx context.Context, name string, cidrs, ports []string) error {
	return m.mutateRule(ctx, name, false, func(r *Rule) error {
		if err := r.AddCIDRs(cidrs); err != nil {
			return err
		}
		return r.AddPorts(ports)
	})
}

// Remove drops CIDRs and/or port specs from the named rule and re-renders.
// Removing an absent entry is a no-op. force authorises removing the last
// address (or the last port) from the management allowlist; without it that one
// case is refused — see mutate.
func (m *Manager) Remove(ctx context.Context, name string, cidrs, ports []string, force bool) error {
	return m.mutateRule(ctx, name, force, func(r *Rule) error {
		r.RemoveCIDRs(cidrs)
		r.RemovePorts(ports)
		return nil
	})
}

// Update is one rule's replacement membership for SetMany. A nil slice leaves
// that dimension unchanged; an empty (non-nil) slice clears it. Proto and
// ICMPEcho follow the same convention with pointers, since their zero values
// ("" and false) are both meaningful settings rather than "not supplied".
type Update struct {
	Name     string
	CIDRs    []string
	Ports    []string
	Proto    *Proto
	ICMPEcho *bool
}

// Set atomically replaces the named rule's address list and/or port list.
// force authorises replacing the management allowlist (or its port list) with
// an empty one; without it that case is refused — see mutate.
func (m *Manager) Set(ctx context.Context, name string, cidrs, ports []string, force bool) error {
	return m.SetMany(ctx, []Update{{Name: name, CIDRs: cidrs, Ports: ports}}, force)
}

// SetMany applies several rules' replacement membership in a single re-render,
// so a `set` naming more than one block lands as one nft transaction rather than
// several — a half-applied management allowlist is exactly the state worth
// avoiding here.
func (m *Manager) SetMany(ctx context.Context, updates []Update, force bool) error {
	return m.mutate(ctx, force, func(t *Table) error {
		for _, u := range updates {
			r, ok := t.Rule(u.Name)
			if !ok {
				return errorx.IllegalArgument.New(
					"no rule named %q; known rules are %s. Declare a new allow rule with `network firewall create-allow-rule --name %s`",
					u.Name, strings.Join(t.Names(), ", "), u.Name)
			}
			if u.CIDRs != nil {
				if err := r.SetCIDRs(u.CIDRs); err != nil {
					return err
				}
			}
			if u.Ports != nil {
				if err := r.SetPorts(u.Ports); err != nil {
					return err
				}
			}
			if u.Proto != nil {
				r.Proto = *u.Proto
			}
			if u.ICMPEcho != nil {
				r.ICMPEcho = *u.ICMPEcho
			}
		}
		return nil
	})
}

// DeleteRule removes one named allow rule and re-renders. The reserved blocks
// cannot be deleted; see Table.DeleteRule.
func (m *Manager) DeleteRule(ctx context.Context, name string) error {
	// Table.DeleteRule refuses the reserved names, so this can never empty the
	// management allowlist and never needs force.
	return m.mutate(ctx, false, func(t *Table) error { return t.DeleteRule(name) })
}

// Config returns the declarative config of the currently-configured table, for
// `show --output yaml`. Unlike Show it reads the persisted config rather than
// the kernel, so its output is the same shape that produced the ruleset — a
// kernel dump has already lost the distinction between an authored rule and a
// default, and auto-merge may have rewritten the port sets.
func (m *Manager) Config(ctx context.Context) (*FileConfig, error) {
	t, err := m.Table(ctx)
	if err != nil {
		return nil, err
	}
	return FileConfigFromTable(t), nil
}

// Table returns the currently-configured table. Read-only: no lock is taken,
// because a torn read cannot happen — the config is replaced by rename.
func (m *Manager) Table(_ context.Context) (*Table, error) {
	return m.load()
}

// IsActive reports whether the inet weaver-host-firewall table is currently present in the
// kernel. It is a read-only probe (no lock, no mutation) used by callers that
// need the firewall's current on-host state, rather than a recorded decision
// whose absence cannot be distinguished from "disabled":
// common.ResolveFirewallSeed seeds the reconfigure enable/disable choice from it
// so an active firewall is never torn down by default, and NetworkFirewallCreate
// uses it to scope its rollback to a table that step actually introduced.
func (m *Manager) IsActive(ctx context.Context) (bool, error) {
	return m.runner.Exists(ctx)
}

// Show returns the live inet weaver-host-firewall table. If the table is not active it returns
// a human-readable message (not an error) so the caller can print it cleanly.
func (m *Manager) Show(ctx context.Context) (string, error) {
	exists, err := m.runner.Exists(ctx)
	if err != nil {
		return "", err
	}
	if !exists {
		return "No inet weaver-host-firewall firewall table is currently active.\n" +
			"Run `solo-provisioner network firewall create` to install one.", nil
	}
	return m.runner.List(ctx)
}

// Delete removes the inet weaver-host-firewall table and its on-disk artifact. It is
// idempotent. It deliberately does NOT disable the shared
// solo-provisioner-network-nft.service (shared with inet weaver-workload-policy) — that is
// orchestrated by `kube cluster uninstall` (#791).
func (m *Manager) Delete(ctx context.Context) error {
	return m.withLock(func() error {
		exists, err := m.runner.Exists(ctx)
		if err != nil {
			return err
		}
		if exists {
			if err := m.runner.Delete(ctx); err != nil {
				return err
			}
		}
		// The retained generation goes with them: left behind, a later `create` on
		// this host would inherit a "previous" config belonging to a table that no
		// longer exists.
		for _, p := range []string{m.nftPath, m.configPath, m.prevConfigPath} {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return errorx.ExternalError.Wrap(err, "failed to remove %s", p)
			}
		}
		return nil
	})
}

// Reapply re-renders and re-applies the persisted config without changing it.
// It is what re-asserts the weaver-managed table on demand: the operator states
// no intent, so there is nothing to supply and nothing to override.
//
// Implemented as a no-op mutation because that is exactly the semantics wanted —
// load the persisted table, dry-run it, rewrite the artifacts, restart the unit.
// The rendered document carries the scoped-replace prefix, so this flushes and
// reloads `inet weaver-host-firewall` alone and leaves any third-party table on
// the host untouched.
//
// No separate "nothing is persisted" guard: load() already fails with a pointer
// to `create` when neither the config nor the nft artifact exists, and never
// falls back to a default table — applying a default-drop policy with an empty
// management allowlist would be a lock-out. Validation is likewise load()'s,
// which parses through ParseConfig and so runs Table.Validate.
func (m *Manager) Reapply(ctx context.Context) error {
	// A no-op mutation: the lock-out guard compares before against after and so
	// can never fire here, even on a table whose allowlist is already empty.
	return m.mutate(ctx, false, func(*Table) error { return nil })
}

// mutate loads the current table from disk, applies fn, then re-applies and
// re-persists the full table under the shared lock.
//
// It also carries the management lock-out guard (#1034): a mutation that takes
// the mgmt rule from reachable to unreachable is refused unless force is set —
// see checkMgmtLockout. The check lives here because this is the only layer that
// sees both the prior and the resulting table — fn mutates t in place — and
// because mutate is reached by exactly the verbs that can empty a populated
// allowlist. Create/Apply/CreateRule do not pass through here and keep their
// warn-only behaviour (see applyAndPersist).
func (m *Manager) mutate(ctx context.Context, force bool, fn func(*Table) error) error {
	return m.withLock(func() error {
		t, err := m.load()
		if err != nil {
			return err
		}
		// Captured before fn runs: fn mutates t in place.
		cidrsBefore, portsBefore := len(t.Mgmt.CIDRs), len(t.Mgmt.Ports)
		if err := fn(t); err != nil {
			return err
		}
		if err := checkMgmtLockout(t, cidrsBefore, portsBefore, force); err != nil {
			return err
		}
		return m.applyAndPersist(ctx, t)
	})
}

// checkMgmtLockout refuses a mutation that takes the management rule from
// reachable to unreachable. The rule renders unconditionally as
// `saddr @mgmt_addrs tcp dport @mgmt_ports accept`, so either half empty makes
// it match no packet and, under the input chain's default-drop policy, the host
// drops every new SSH connection — while the operator's current session survives
// on the established-connection accept and hides the damage (#1034).
//
// Only that transition is guarded, not the state: a rule that is already
// unreachable — either half already empty — stays fully mutable, so an operator
// repairing one is never blocked from editing the rest of the table. A refusal
// returns before applyAndPersist, so nothing is rendered, dry-run or written.
func checkMgmtLockout(t *Table, cidrsBefore, portsBefore int, force bool) error {
	if force || cidrsBefore == 0 || portsBefore == 0 {
		return nil
	}
	if len(t.Mgmt.CIDRs) == 0 {
		return errx.Decorate(
			errorx.IllegalArgument.New(
				"refusing to empty the management address list: an empty @mgmt_addrs matches no source under the "+
					"default-drop input chain, so this host would drop every new SSH connection — your current session "+
					"survives on the established-connection accept and will not show the loss. Nothing was changed"),
			reasons.InvalidArgument,
			"Supply a replacement list: `solo-provisioner network firewall set --name mgmt --cidrs <cidr,...>`",
			"Pass --force to empty it anyway")
	}
	if len(t.Mgmt.Ports) == 0 {
		return errx.Decorate(
			errorx.IllegalArgument.New(
				"refusing to empty the management port list: the management accept renders `dport @mgmt_ports` "+
					"unconditionally, so an empty port set locks this host out of new SSH connections exactly like an "+
					"empty address list. Nothing was changed"),
			reasons.InvalidArgument,
			"Supply a replacement list: `solo-provisioner network firewall set --name mgmt --ports <port,...>`",
			"Pass --force to empty it anyway")
	}
	return nil
}

// mutateRule resolves name to a rule and applies fn to it. An unknown name is
// rejected with the valid names listed, rather than silently creating a rule:
// declaring a rule is its own verb, so a mistyped --name edits nothing instead
// of quietly creating a second rule alongside the one that was meant.
func (m *Manager) mutateRule(ctx context.Context, name string, force bool, fn func(*Rule) error) error {
	return m.mutate(ctx, force, func(t *Table) error {
		r, ok := t.Rule(name)
		if !ok {
			return errorx.IllegalArgument.New(
				"no rule named %q; known rules are %s. Declare a new allow rule with `network firewall create-allow-rule --name %s`",
				name, strings.Join(t.Names(), ", "), name)
		}
		return fn(r)
	})
}

// applyAndPersist dry-runs the rendered ruleset, then atomically rewrites the
// declarative config and the nft artifact, then restarts the systemd service via
// DBus so the kernel picks up the new rules. The rendered file contains the
// idempotent scoped-replace prefix, so it is safe for both the boot-time oneshot
// and live re-applies.
//
// The dry run comes first, and nothing is written unless it passes. The unit has
// no ExecStop, so a ruleset nft refuses leaves the live table untouched and the
// failure looks harmless — but persisting first would have made that document
// the boot artifact, and the host would come up with no weaver firewall at all
// (#1002). Validating up front means a rejected ruleset never reaches disk.
//
// Past the dry run, the config is written before the nft artifact: it is what
// the next mutation loads, so a crash between the two writes leaves the
// operator's intent recorded and the kernel merely stale, which the next apply
// fixes. The reverse order would lose the intent while leaving the ruleset live,
// and there would be nothing left to re-derive it from.
func (m *Manager) applyAndPersist(ctx context.Context, t *Table) error {
	block, err := t.Render()
	if err != nil {
		return err
	}

	// Declaring a rule before populating it is supported, so this is not an
	// error — but a rule that grants nothing is indistinguishable from a
	// finished one in `show`, and a half-run declare sequence is the likeliest
	// way to end up here.
	if names := t.IncompleteAllowRules(); len(names) > 0 {
		logx.As().Warn().Strs("rules", names).Msg(
			"allow rule(s) render nothing yet: each needs at least one CIDR and either a port or icmp_echo — populate with `network firewall add --name <rule> --cidr <cidr> --port <port>`")
	}

	// The management accept renders even with empty sets, so applying a table
	// with either half empty drops every new SSH connection to this host.
	if len(t.Mgmt.CIDRs) == 0 || len(t.Mgmt.Ports) == 0 {
		logx.As().Warn().Msg(
			"the management allow rule matches nothing: its address or port list is empty and the input chain is " +
				"policy drop, so new SSH connections to this host are dropped — your current session survives on " +
				"the established-connection accept and will not show this. Restore it with `network firewall set " +
				"--name mgmt --cidrs <cidr,...>` (or --ports)")
	}

	cfg, err := FileConfigFromTable(t).Marshal()
	if err != nil {
		return err
	}

	if err := m.check(ctx, block); err != nil {
		return err
	}

	m.retainPreviousConfig()

	if err := atomicWriteFile(m.configPath, string(cfg), 0o600); err != nil {
		return err
	}

	if err := atomicWriteFile(m.nftPath, block, 0o644); err != nil {
		return err
	}

	return m.applyViaService(ctx)
}

// retainPreviousConfig copies the config about to be replaced to
// prevConfigPath, so a state file that is later lost or truncated can be
// recovered without falling through to the nft reparse — the tier that recovers
// the reserved blocks but loses every named allow rule.
//
// Three deliberate properties:
//
// It copies the bytes on disk, not the table being applied. The point is the
// literal previous generation; the in-memory table may itself have come from the
// lossy reparse, in which case retaining it would record a table the operator
// never authored.
//
// It retains only a config that parses. The invariant is "absent, or a loadable
// config exactly one generation back", which is what makes the retained copy
// worth trusting in a recovery. It also means recovering *from* prevConfigPath
// does not consume it: the corrupt config that prompted the recovery is not
// promoted over the good one.
//
// It never fails the apply. The ruleset has already passed the dry run at this
// point, so refusing to apply a valid firewall because a bookkeeping write
// failed would be the worse outcome. A stale copy is worse than none, though —
// it would silently restore an older generation than the operator expects — so a
// failed write removes any copy left behind rather than leaving it in place.
func (m *Manager) retainPreviousConfig() {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		// No config yet (first apply, or a host recovered from the nft artifact):
		// there is no previous generation, and inventing one would be a lie.
		if !os.IsNotExist(err) {
			logx.As().Debug().Err(err).Str("path", m.configPath).Msg(
				"could not read the current host firewall config; no previous generation retained")
		}
		return
	}

	if _, err := ParseConfig(data); err != nil {
		logx.As().Debug().Err(err).Str("path", m.configPath).Msg(
			"the current host firewall config does not parse; keeping the existing retained copy rather than replacing it with an unusable one")
		return
	}

	if err := atomicWriteFile(m.prevConfigPath, string(data), 0o600); err != nil {
		logx.As().Warn().Err(err).Str("path", m.prevConfigPath).Msg(
			"could not retain the previous host firewall config; removing any stale copy so it cannot be mistaken for the generation just replaced")
		if rmErr := os.Remove(m.prevConfigPath); rmErr != nil && !os.IsNotExist(rmErr) {
			logx.As().Warn().Err(rmErr).Str("path", m.prevConfigPath).Msg(
				"could not remove the stale retained config; do not recover from it without checking its contents first")
		}
	}
}

// check dry-runs a rendered ruleset through nft without committing it. The
// document is staged in the nft artifact's own directory rather than the system
// temp dir, so the check runs on the same filesystem and permissions the real
// load will see, and is removed either way.
func (m *Manager) check(ctx context.Context, block string) error {
	dir := filepath.Dir(m.nftPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to create directory %s", dir)
	}

	staged, err := os.CreateTemp(dir, ".network-weaver-host-firewall-*.nft.check")
	if err != nil {
		return errorx.ExternalError.Wrap(err, "failed to create temp file in %s", dir)
	}
	name := staged.Name()
	defer func() { _ = os.Remove(name) }()

	if _, err := staged.WriteString(block); err != nil {
		_ = staged.Close()
		return errorx.ExternalError.Wrap(err, "failed to write temp file %s", name)
	}
	if err := staged.Close(); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to close temp file %s", name)
	}

	if err := m.runner.Check(ctx, name); err != nil {
		return errorx.Decorate(err, "the host firewall ruleset was not applied and nothing was written to %s or %s", m.configPath, m.nftPath)
	}
	return nil
}

// load returns the currently-configured table. The declarative config is the
// source of truth; the rendered nft artifact is a fallback for a host
// provisioned before the config file existed, or one that lost it. See Parse for
// what the fallback can and cannot recover.
func (m *Manager) load() (*Table, error) {
	data, err := os.ReadFile(m.configPath)
	switch {
	case err == nil:
		cfg, err := ParseConfig(data)
		if err != nil {
			return nil, errorx.Decorate(err, "failed to load %s", m.configPath)
		}
		return cfg.Table()
	case !os.IsNotExist(err):
		return nil, errorx.ExternalError.Wrap(err, "failed to read %s", m.configPath)
	}

	nft, err := os.ReadFile(m.nftPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errorx.IllegalState.New("inet weaver-host-firewall firewall not found at %s; run `solo-provisioner network firewall create` first", m.configPath)
		}
		return nil, errorx.ExternalError.Wrap(err, "failed to read %s", m.nftPath)
	}
	logx.As().Info().Str("path", m.nftPath).Msg(
		"no host firewall config file; recovering the reserved blocks from the rendered ruleset — named allow rules are not recoverable and must be re-declared with `network firewall create-allow-rule` and then re-populated with `network firewall add`")
	return Parse(string(nft))
}

// withLock serialises a mutation behind the shared cross-command flock so a
// hand-run operator command and the daemon poll loop (#754) cannot interleave
// nft transactions.
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
