// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"context"
	"crypto/sha256"
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

// Manager implements the `network shape` verb: create, set, show, and delete
// operations for tc HTB device roots and per-class bandwidth configurations.
//
// Egress mutations (Dir "egress") re-render TcEgressScriptPath and restart the
// tc-egress.service oneshot so the kernel picks up changes immediately. Ingress
// mutations (Dir "ingress") only write config; the daemon pod-lifecycle watcher
// The daemon pod-lifecycle watcher reads them and applies them to each new veth interface.
type Manager struct {
	scriptPath  string
	lockPath    string
	nicDetect   func() (string, error)
	applyEgress func(ctx context.Context) error
	tcRunner    TCRunner
}

// Config customises a Manager. The zero value is not useful; prefer NewManager.
type Config struct {
	ScriptPath  string
	LockPath    string
	NICDetect   func() (string, error)
	ApplyEgress func(ctx context.Context) error
	TCRunner    TCRunner
}

// NewManager returns a Manager wired to the live kernel and production paths.
func NewManager() *Manager {
	return NewManagerWithConfig(Config{})
}

// NewManagerWithConfig returns a Manager, filling unset Config fields with
// their production defaults.
func NewManagerWithConfig(cfg Config) *Manager {
	m := &Manager{
		scriptPath:  cfg.ScriptPath,
		lockPath:    cfg.LockPath,
		nicDetect:   cfg.NICDetect,
		applyEgress: cfg.ApplyEgress,
		tcRunner:    cfg.TCRunner,
	}
	if m.scriptPath == "" {
		m.scriptPath = TcEgressScriptPath
	}
	if m.lockPath == "" {
		m.lockPath = ShapeLockPath
	}
	if m.nicDetect == nil {
		m.nicDetect = DetectEgressInterface
	}
	if m.applyEgress == nil {
		m.applyEgress = ApplyTcEgressScript
	}
	if m.tcRunner == nil {
		m.tcRunner = newExecTCRunner()
	}
	return m
}

// CreateDevice creates (or replaces with --force) the root device configuration.
// For "egress": re-renders TcEgressScriptPath and restarts tc-egress.service.
// For "ingress": writes config only (daemon pod-lifecycle watcher handles VETH apply).
//
// Returns true if the config was created or replaced, false if it already
// existed and force was not set.
func (m *Manager) CreateDevice(ctx context.Context, dev *DeviceConfig, force bool) (bool, error) {
	if err := validateDir(dev.Dir); err != nil {
		return false, err
	}
	if err := validateRate(dev.Rate); err != nil {
		return false, err
	}
	if err := validateDefaultClass(dev.DefaultClass, dev.Dir); err != nil {
		return false, err
	}

	var changed bool
	err := m.withLock(func() error {
		existing, err := readDevice(dev.Dir)
		if err != nil {
			return err
		}
		if existing != nil && !force {
			logx.As().Warn().Str("dir", dev.Dir).Msg(
				"network shape device already configured — flags not applied; pass --force to replace")
			return nil
		}
		if existing != nil {
			dev.CreatedAt = existing.CreatedAt
		} else if dev.CreatedAt.IsZero() {
			dev.CreatedAt = time.Now().UTC()
		}
		if err := writeDevice(dev); err != nil {
			return err
		}
		if dev.Dir == DirEgress {
			if err := m.renderAndApplyEgress(ctx); err != nil {
				return err
			}
		}
		changed = true
		return nil
	})
	return changed, err
}

// CreateClass creates (or replaces with --force) a per-class bandwidth configuration.
// The device for the class direction must exist before adding classes.
// For egress classes: re-renders TcEgressScriptPath and restarts tc-egress.service.
// For ingress classes: writes config only (daemon pod-lifecycle watcher handles VETH apply).
//
// Returns true if the config was created or replaced, false if it already
// existed and force was not set.
func (m *Manager) CreateClass(ctx context.Context, cls *ClassConfig, force bool) (bool, error) {
	ci, err := lookupClassInfo(cls.Name)
	if err != nil {
		return false, err
	}
	if err := validateRate(cls.Rate); err != nil {
		return false, err
	}
	if cls.Ceil != "" {
		if err := validateCeilGeRate(cls.Ceil, cls.Rate); err != nil {
			return false, err
		}
	}
	if err := validatePrio(cls.Prio); err != nil {
		return false, err
	}

	var changed bool
	err = m.withLock(func() error {
		dev, err := readDevice(ci.Dir)
		if err != nil {
			return err
		}
		if dev == nil {
			return errorx.IllegalState.New(
				"device %q is not configured; run `network shape create --device %s` first",
				ci.Dir, ci.Dir)
		}

		siblings, err := loadClassesForDir(ci.Dir)
		if err != nil {
			return err
		}
		if err := validateSumRates(siblings, cls, dev.Rate); err != nil {
			return err
		}

		existing, err := readClass(cls.Name)
		if err != nil {
			return err
		}
		if existing != nil && !force {
			logx.As().Warn().Str("class", cls.Name).Msg(
				"network shape class already configured — flags not applied; pass --force to replace")
			return nil
		}
		if existing != nil {
			cls.CreatedAt = existing.CreatedAt
		} else if cls.CreatedAt.IsZero() {
			cls.CreatedAt = time.Now().UTC()
		}
		if err := writeClass(cls); err != nil {
			return err
		}
		if ci.Dir == DirEgress {
			if err := m.renderAndApplyEgress(ctx); err != nil {
				return err
			}
		}
		changed = true
		return nil
	})
	return changed, err
}

// SetClass atomically updates one or more bandwidth parameters of an existing
// class. Only non-nil pointer fields are changed; nil means "keep current value".
// For egress classes: runs `tc class change` on the live kernel and re-renders
// the boot script for reboot persistence. For ingress classes: updates config only.
func (m *Manager) SetClass(ctx context.Context, name string, rate, ceil *string, prio *int) error {
	ci, err := lookupClassInfo(name)
	if err != nil {
		return err
	}
	return m.withLock(func() error {
		cls, err := readClass(name)
		if err != nil {
			return err
		}
		if cls == nil {
			return errorx.IllegalState.New(
				"class %q is not configured; run `network shape create --class %s` first", name, name)
		}
		if rate != nil {
			cls.Rate = *rate
		}
		if ceil != nil {
			cls.Ceil = *ceil
		}
		if prio != nil {
			cls.Prio = *prio
		}

		// Validate the updated state.
		if err := validateRate(cls.Rate); err != nil {
			return err
		}
		if cls.Ceil != "" {
			if err := validateCeilGeRate(cls.Ceil, cls.Rate); err != nil {
				return err
			}
		}
		if err := validatePrio(cls.Prio); err != nil {
			return err
		}

		dev, err := readDevice(ci.Dir)
		if err != nil {
			return err
		}
		if dev != nil {
			siblings, err := loadClassesForDir(ci.Dir)
			if err != nil {
				return err
			}
			if err := validateSumRates(siblings, cls, dev.Rate); err != nil {
				return err
			}
		}

		if err := writeClass(cls); err != nil {
			return err
		}

		if ci.Dir == DirEgress {
			nic, err := m.nicDetect()
			if err != nil {
				return errorx.Decorate(err, "cannot apply live tc class change: egress NIC detection failed")
			}
			if err := m.tcRunner.ClassChange(ctx, nic, ci.Minor, cls.Rate, cls.effectiveCeil(), cls.Prio); err != nil {
				return errorx.Decorate(err,
					"class config updated on disk but live tc class change failed; reboot or restart tc-egress.service to sync")
			}
			if err := m.renderAndApplyScript(ctx, nic); err != nil {
				return errorx.Decorate(err,
					"live tc class change applied but boot script re-render failed; reboot may revert the change")
			}
		}
		return nil
	})
}

// ShowClass returns a human-readable summary of the named class config.
func (m *Manager) ShowClass(name string) (string, error) {
	cls, err := readClass(name)
	if err != nil {
		return "", err
	}
	if cls == nil {
		return "", errorx.IllegalState.New("class %q is not configured", name)
	}
	ci, _ := lookupClassInfo(name)
	var b strings.Builder
	fmt.Fprintf(&b, "class %s (1:%s, %s device)\n", cls.Name, ci.Minor, ci.Dir)
	fmt.Fprintf(&b, "  rate:      %s\n", cls.Rate)
	fmt.Fprintf(&b, "  ceil:      %s\n", cls.effectiveCeil())
	fmt.Fprintf(&b, "  prio:      %d\n", cls.Prio)
	fmt.Fprintf(&b, "  created:   %s\n", cls.CreatedAt.Format(time.RFC3339))
	return b.String(), nil
}

// ShowDevice returns a human-readable summary of the named device config.
func (m *Manager) ShowDevice(dir string) (string, error) {
	if err := validateDir(dir); err != nil {
		return "", err
	}
	dev, err := readDevice(dir)
	if err != nil {
		return "", err
	}
	if dev == nil {
		return "", errorx.IllegalState.New("device %q is not configured", dir)
	}
	classes, err := loadClassesForDir(dir)
	if err != nil {
		return "", err
	}
	sort.Slice(classes, func(i, j int) bool {
		return classes[i].Name < classes[j].Name
	})
	var b strings.Builder
	fmt.Fprintf(&b, "device %s\n", dev.Dir)
	fmt.Fprintf(&b, "  rate:    %s\n", dev.Rate)
	fmt.Fprintf(&b, "  default: %s\n", dev.DefaultClass)
	fmt.Fprintf(&b, "  created: %s\n", dev.CreatedAt.Format(time.RFC3339))
	if len(classes) > 0 {
		fmt.Fprintf(&b, "  classes:\n")
		for _, cls := range classes {
			ci, _ := lookupClassInfo(cls.Name)
			fmt.Fprintf(&b, "    %-20s rate=%-10s ceil=%-10s prio=%d (1:%s)\n",
				cls.Name, cls.Rate, cls.effectiveCeil(), cls.Prio, ci.Minor)
		}
	}
	return b.String(), nil
}

// ShowAll returns a human-readable summary of all configured devices and classes.
func (m *Manager) ShowAll() (string, error) {
	var b strings.Builder
	for _, dir := range []string{DirEgress, DirIngress} {
		dev, err := readDevice(dir)
		if err != nil {
			return "", err
		}
		if dev == nil {
			continue // not configured
		}
		out, err := m.ShowDevice(dir)
		if err != nil {
			return "", err
		}
		b.WriteString(out)
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		return "no shape configuration found\n", nil
	}
	return b.String(), nil
}

// DeleteClass removes a class configuration. Fails if the class is referenced
// as the device's default class or by any policy's --stamp/--reply-stamp.
// For egress classes: re-renders TcEgressScriptPath and restarts tc-egress.service.
func (m *Manager) DeleteClass(ctx context.Context, name string) error {
	ci, err := lookupClassInfo(name)
	if err != nil {
		return err
	}
	return m.withLock(func() error {
		cls, err := readClass(name)
		if err != nil {
			return err
		}
		if cls == nil {
			return errorx.IllegalState.New("class %q is not configured", name)
		}

		// Block deletion if this class is the device default.
		dev, err := readDevice(ci.Dir)
		if err != nil {
			return err
		}
		if dev != nil && dev.DefaultClass == name {
			return errorx.IllegalState.New(
				"class %q is the default class for device %q and cannot be deleted; "+
					"reconfigure the device with a different --default first", name, ci.Dir)
		}

		// Block deletion if any policy stamps this class.
		stamps, err := loadPolicyStamps()
		if err != nil {
			return err
		}
		if refs := stamps[name]; len(refs) > 0 {
			return errorx.IllegalState.New(
				"class %q is referenced by network policy %s and cannot be deleted; "+
					"delete or update those policies first", name, strings.Join(refs, ", "))
		}

		if err := removeClass(name); err != nil {
			return err
		}
		if ci.Dir == DirEgress {
			if err := m.renderAndApplyEgress(ctx); err != nil {
				return errorx.Decorate(err,
					"class config deleted but boot script re-render failed; restart tc-egress.service to sync")
			}
		}
		return nil
	})
}

// DeleteDevice removes a device configuration. Fails if any classes are still
// configured for this device (delete classes first).
func (m *Manager) DeleteDevice(ctx context.Context, dir string) error {
	if err := validateDir(dir); err != nil {
		return err
	}
	return m.withLock(func() error {
		dev, err := readDevice(dir)
		if err != nil {
			return err
		}
		if dev == nil {
			return errorx.IllegalState.New("device %q is not configured", dir)
		}
		classes, err := loadClassesForDir(dir)
		if err != nil {
			return err
		}
		if len(classes) > 0 {
			names := make([]string, 0, len(classes))
			for _, c := range classes {
				names = append(names, c.Name)
			}
			return errorx.IllegalState.New(
				"device %q still has configured classes (%s); delete them first",
				dir, strings.Join(names, ", "))
		}
		if err := removeDevice(dir); err != nil {
			return err
		}
		if dir == DirEgress {
			// Re-render with default (empty) egress config.
			if err := m.renderAndApplyEgress(ctx); err != nil {
				return errorx.Decorate(err,
					"device config deleted but boot script re-render failed; restart tc-egress.service to sync")
			}
		}
		return nil
	})
}

// renderAndApplyEgress detects the egress NIC, re-renders the boot script from
// stored config (or the default if no device config exists), and applies via
// service restart.
func (m *Manager) renderAndApplyEgress(ctx context.Context) error {
	nic, err := m.nicDetect()
	if err != nil {
		return errorx.Decorate(err, "cannot re-render tc-egress script: egress NIC detection failed")
	}
	return m.renderAndApplyScript(ctx, nic)
}

// renderAndApplyScript renders the tc-egress script with the given NIC name
// (using stored config if available, else the default) and restarts the service.
func (m *Manager) renderAndApplyScript(ctx context.Context, nic string) error {
	dev, err := readDevice(DirEgress)
	if err != nil {
		return err
	}
	var rendered string
	if dev != nil {
		classes, err := loadClassesForDir(DirEgress)
		if err != nil {
			return err
		}
		if len(classes) == 0 {
			// Device configured but no classes yet: render the default SPEED-based
			// script so the boot oneshot always runs a self-consistent hierarchy.
			rendered, err = renderTcEgressScript(nic)
		} else {
			rendered, err = renderTcEgressScriptFromConfig(nic, dev, classes)
		}
		if err != nil {
			return err
		}
	} else {
		rendered, err = renderTcEgressScript(nic)
		if err != nil {
			return err
		}
	}

	// Skip the write when on-disk content is already identical.
	if existing, readErr := os.ReadFile(m.scriptPath); readErr == nil {
		if sha256.Sum256([]byte(rendered)) == sha256.Sum256(existing) {
			return m.applyEgress(ctx)
		}
	}
	if err := atomicWriteFile(m.scriptPath, rendered, 0o755); err != nil {
		return err
	}
	return m.applyEgress(ctx)
}

// defaultEgressConfig returns the device root and three default egress classes
// at proportions derived from trunkRate (partner 40%/70%, public 30%/70%,
// reserve-egress 30%/100%). Exposed as a package-internal helper so tests can
// verify the computation without disk I/O.
func defaultEgressConfig(trunkRate string) (*DeviceConfig, []*ClassConfig, error) {
	bps, err := parseBandwidthBps(trunkRate)
	if err != nil {
		return nil, nil, errorx.IllegalArgument.Wrap(err, "invalid trunk rate %q", trunkRate)
	}
	mbps := bps / 1_000_000
	now := time.Now().UTC()
	dev := &DeviceConfig{
		Dir:          DirEgress,
		Rate:         trunkRate,
		DefaultClass: "reserve-egress",
		CreatedAt:    now,
	}
	classes := []*ClassConfig{
		{Name: "partner", Rate: fmt.Sprintf("%dmbit", mbps*40/100), Ceil: fmt.Sprintf("%dmbit", mbps*70/100), Prio: 0, CreatedAt: now},
		{Name: "public", Rate: fmt.Sprintf("%dmbit", mbps*30/100), Ceil: fmt.Sprintf("%dmbit", mbps*70/100), Prio: 5, CreatedAt: now},
		{Name: "reserve-egress", Rate: fmt.Sprintf("%dmbit", mbps*30/100), Ceil: trunkRate, Prio: 1, CreatedAt: now},
	}
	return dev, classes, nil
}

// ProvisionDefaultEgress configures the egress device root and three default
// HTB classes at proportions derived from trunkRate (partner 40%/70%, public
// 30%/70%, reserve-egress 30%/100%), then renders and applies the boot script.
// Existing configs are always replaced. Called by block node install so the
// shape registry is the single source of truth from first install.
func (m *Manager) ProvisionDefaultEgress(ctx context.Context, nicName, trunkRate string) error {
	dev, classes, err := defaultEgressConfig(trunkRate)
	if err != nil {
		return err
	}
	return m.withLock(func() error {
		if err := writeDevice(dev); err != nil {
			return err
		}
		for _, cls := range classes {
			if err := writeClass(cls); err != nil {
				return err
			}
		}
		return m.renderAndApplyScript(ctx, nicName)
	})
}

// RenderAndApplyEgress renders the tc-egress boot script for nic from stored
// shape config (if available) or the sysfs auto-detect default, then installs
// and restarts tc-egress.service. Idempotent.
func (m *Manager) RenderAndApplyEgress(ctx context.Context, nic string) error {
	return m.renderAndApplyScript(ctx, nic)
}

// ProvisionDefaultEgressShape configures the egress shape registry with the
// three default HTB classes at proportions derived from trunkRate, then renders
// and applies the boot script. Convenience wrapper over NewManager().ProvisionDefaultEgress.
func ProvisionDefaultEgressShape(ctx context.Context, nicName, trunkRate string) error {
	return NewManager().ProvisionDefaultEgress(ctx, nicName, trunkRate)
}

// RenderAndApplyDefaultEgress renders the tc-egress script for nic from the
// shape registry (or sysfs fallback when no config exists) and applies it.
// Used when no trunk rate is supplied (e.g. block node reconfigure without
// --link-rate).
func RenderAndApplyDefaultEgress(ctx context.Context, nic string) error {
	return NewManager().RenderAndApplyEgress(ctx, nic)
}

// withLock serialises a mutation behind a cross-command flock so concurrent
// operator commands cannot interleave tc transactions.
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
