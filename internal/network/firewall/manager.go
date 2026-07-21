// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"context"
	"os"
	"path/filepath"
	"syscall"

	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
)

// Manager implements the `network firewall` verbs against the `inet host`
// table. Every mutating verb takes the shared apply lock, atomically rewrites
// the on-disk artifact, and then restarts the systemd service via DBus so the
// kernel is updated in one consistent operation — no separate nft apply exec.
type Manager struct {
	runner          Runner
	nftPath         string
	lockPath        string
	applyViaService func(ctx context.Context) error
}

// Config customises a Manager. The zero value is not useful; prefer NewManager.
// Tests inject a fake Runner, temp paths, and a no-op service func so the
// package builds and runs on any platform.
type Config struct {
	Runner          Runner
	NftPath         string
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
		lockPath:        cfg.LockPath,
		applyViaService: cfg.ApplyViaService,
	}
	if m.runner == nil {
		m.runner = NewExecRunner()
	}
	if m.nftPath == "" {
		m.nftPath = HostNftPath
	}
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
			logx.As().Warn().Str("table", TableName).Msg("inet host firewall already exists — supplied flags were not applied; pass --force to re-render from the current flags")
			return nil
		}
		changed = true
		return m.applyAndPersist(ctx, t)
	})
	return changed, err
}

// AddMgmtCIDR adds one CIDR to the management allowlist and re-renders.
func (m *Manager) AddMgmtCIDR(ctx context.Context, cidr string) error {
	return m.mutate(ctx, func(t *Table) error { return t.AddMgmtCIDR(cidr) })
}

// RemoveMgmtCIDR removes one CIDR from the management allowlist and re-renders.
func (m *Manager) RemoveMgmtCIDR(ctx context.Context, cidr string) error {
	return m.mutate(ctx, func(t *Table) error { t.RemoveMgmtCIDR(cidr); return nil })
}

// AddBlockedCIDR adds one CIDR to the operator block list and re-renders.
func (m *Manager) AddBlockedCIDR(ctx context.Context, cidr string) error {
	return m.mutate(ctx, func(t *Table) error { return t.AddBlockedCIDR(cidr) })
}

// RemoveBlockedCIDR removes one CIDR from the operator block list and re-renders.
func (m *Manager) RemoveBlockedCIDR(ctx context.Context, cidr string) error {
	return m.mutate(ctx, func(t *Table) error { t.RemoveBlockedCIDR(cidr); return nil })
}

// AddPort adds one in-cluster host-service port and re-renders.
func (m *Manager) AddPort(ctx context.Context, port int) error {
	return m.mutate(ctx, func(t *Table) error { return t.AddPort(port) })
}

// RemovePort removes one in-cluster host-service port and re-renders.
func (m *Manager) RemovePort(ctx context.Context, port int) error {
	return m.mutate(ctx, func(t *Table) error { t.RemovePort(port); return nil })
}

// Set atomically replaces the management CIDR list, the operator block list,
// and/or the in-cluster port list. A nil slice leaves that dimension unchanged;
// an empty (non-nil) slice clears it.
func (m *Manager) Set(ctx context.Context, mgmtCIDRs, blockedCIDRs []string, ports []int) error {
	return m.mutate(ctx, func(t *Table) error {
		if mgmtCIDRs != nil {
			if err := t.SetMgmtCIDRs(mgmtCIDRs); err != nil {
				return err
			}
		}
		if blockedCIDRs != nil {
			if err := t.SetBlockedCIDRs(blockedCIDRs); err != nil {
				return err
			}
		}
		if ports != nil {
			if err := t.SetPorts(ports); err != nil {
				return err
			}
		}
		return nil
	})
}

// Show returns the live inet host table. If the table is not active it returns
// a human-readable message (not an error) so the caller can print it cleanly.
func (m *Manager) Show(ctx context.Context) (string, error) {
	exists, err := m.runner.Exists(ctx)
	if err != nil {
		return "", err
	}
	if !exists {
		return "No inet host firewall table is currently active.\n" +
			"Run `solo-provisioner network firewall create` to install one.", nil
	}
	return m.runner.List(ctx)
}

// Delete removes the inet host table and its on-disk artifact. It is
// idempotent. It deliberately does NOT disable the shared
// solo-provisioner-network-nft.service (shared with inet weaver) — that is
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
		if err := os.Remove(m.nftPath); err != nil && !os.IsNotExist(err) {
			return errorx.ExternalError.Wrap(err, "failed to remove %s", m.nftPath)
		}
		return nil
	})
}

// mutate loads the current table from disk, applies fn, then re-applies and
// re-persists the full table under the shared lock.
func (m *Manager) mutate(ctx context.Context, fn func(*Table) error) error {
	return m.withLock(func() error {
		t, err := m.load()
		if err != nil {
			return err
		}
		if err := fn(t); err != nil {
			return err
		}
		return m.applyAndPersist(ctx, t)
	})
}

// applyAndPersist atomically rewrites the on-disk artifact and then restarts
// the systemd service via DBus so the kernel picks up the new rules. The
// rendered file already contains the idempotent `add table / flush table`
// prefix, so it is safe for both the boot-time oneshot and live re-applies.
func (m *Manager) applyAndPersist(ctx context.Context, t *Table) error {
	block, err := t.Render()
	if err != nil {
		return err
	}

	if err := atomicWriteFile(m.nftPath, block, 0o644); err != nil {
		return err
	}

	return m.applyViaService(ctx)
}

func (m *Manager) load() (*Table, error) {
	data, err := os.ReadFile(m.nftPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errorx.IllegalState.New("inet host firewall not found at %s; run `solo-provisioner network firewall create` first", m.nftPath)
		}
		return nil, errorx.ExternalError.Wrap(err, "failed to read %s", m.nftPath)
	}
	return Parse(string(data))
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
