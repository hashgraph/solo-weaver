// SPDX-License-Identifier: Apache-2.0

// migration_sysctl_network_tuning.go converges the network-tuning sysctl file on
// already-provisioned hosts and clears the fs.file-max ceiling it used to pin.
//
// PR #1185 (#1184) dropped fs.file-max = 2097152 from
// 75-network-performance.conf (it is not a network setting, and on large-memory
// hosts it cuts the open-file limit by several orders of magnitude below the OS
// default) and added net.ipv4.tcp_keepalive_probes = 5 (the file set the other
// two keepalive knobs but not the probe count, so dead peers took longer to
// detect than intended). That template change only reaches fresh installs —
// ConfigureSysctlForKubernetes only runs during 'kube cluster install'. Removing
// the fs.file-max line and reloading also does not by itself reset the live
// value; the kernel keeps it at 2097152 until something writes the old value
// back or the host reboots.
//
// This migration closes both gaps on already-provisioned hosts: it rewrites just
// this one file, reloads all of /etc/sysctl.d (matching the install path's own
// reload semantics, so a later file's override still wins), restores fs.file-max
// from the pre-install value recorded in the sysctl backup (BackupSettings)
// best-effort, and verifies the live tcp_keepalive_probes value actually
// converged.
//
// Registered in cmd/cli/commands/root.go RegisterMigrations() under
// migration.ScopeStartup.

package workflows

import (
	"context"
	"fmt"
	"os"
	"path"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/internal/sysctl"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// sysctlNetworkTuningMinVersion is the CLI release that ships the updated
// 75-network-performance.conf template (#1184/#1185) together with this
// migration. The migration applies when upgrading from a version below this
// boundary to this version or later.
const sysctlNetworkTuningMinVersion = "0.32.1"

const (
	networkPerformanceConfFile = "75-network-performance.conf"

	fileMaxKey = "fs.file-max"

	keepaliveProbesKey       = "net.ipv4.tcp_keepalive_probes"
	wantKeepaliveProbesValue = "5"
)

// networkTuningDestPath is where the rendered file lives on the host. A var, not
// a const, so tests can point it at a temp file instead of /etc/sysctl.d.
var networkTuningDestPath = path.Join(sysctl.EtcSysctlDir, networkPerformanceConfFile)

// Seams so tests can stub the host-facing operations without a real /etc/sysctl.d
// or /proc/sys. Reading the embedded template and the sysctl backup file are left
// as real calls — both are pure file reads, and the backup path already has its
// own test seam via models.SetPaths.
var (
	copyNetworkTuningFile = sysctl.CopyConfigurationFile
	setSysctlValue        = sysctl.Set
	getSysctlValue        = sysctl.Get

	// reloadAllSysctlConfiguration reloads every file in /etc/sysctl.d, matching
	// ConfigureSysctlForKubernetes's own reload semantics (later files win) —
	// reloading only the rewritten file would bypass that ordering.
	reloadAllSysctlConfiguration = sysctl.LoadAllConfiguration
)

// SysctlNetworkTuningMigration converges 75-network-performance.conf on
// already-provisioned hosts and clears the fs.file-max ceiling it used to pin.
type SysctlNetworkTuningMigration struct {
	migration.CLIVersionMigration
}

// NewSysctlNetworkTuningMigration constructs the migration.
func NewSysctlNetworkTuningMigration() *SysctlNetworkTuningMigration {
	return &SysctlNetworkTuningMigration{
		CLIVersionMigration: migration.NewCLIVersionMigration(
			"sysctl-network-tuning-v"+sysctlNetworkTuningMinVersion,
			"Rewrite 75-network-performance.conf on already-provisioned hosts so it stops "+
				"pinning fs.file-max and adds net.ipv4.tcp_keepalive_probes, and restore the live "+
				"fs.file-max value without requiring a reboot (#1184)",
			sysctlNetworkTuningMinVersion,
		),
	}
}

// Execute rewrites the file, reloads all of /etc/sysctl.d, restores fs.file-max
// from the sysctl backup on a best-effort basis, and verifies the live
// tcp_keepalive_probes value converged.
//
// It is self-guarding: it no-ops when the host never had the file (e.g. a
// consensus-node-only host) rather than relying on Applies, which only gates on
// the CLI version boundary. Every step is otherwise idempotent, so a repeated
// invocation within the upgrade window (e.g. after a sibling migration fails and
// the version isn't yet persisted) is harmless.
func (m *SysctlNetworkTuningMigration) Execute(ctx context.Context, mctx *migration.Context) error {
	prev, err := os.ReadFile(networkTuningDestPath)
	if err != nil {
		if os.IsNotExist(err) {
			logx.As().Debug().Str("file", networkTuningDestPath).
				Msg("sysctl network tuning migration: file not present on this host; skipping")
			return nil
		}
		return errorx.IllegalState.Wrap(err, "failed to read the existing %s before overwriting it", networkTuningDestPath)
	}
	mctx.Data.Set(sysctlNetworkTuningPrevContentsKey, prev)

	logx.As().Info().Str("file", networkTuningDestPath).
		Msg("Converging the network tuning sysctl file on this host (#1184)")

	if _, err := copyNetworkTuningFile(networkPerformanceConfFile); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write %s", networkTuningDestPath).
			WithProperty(models.ErrPropertyResolution, []string{
				fmt.Sprintf("Verify %s is writable, then re-run any solo-provisioner command.", sysctl.EtcSysctlDir),
			})
	}

	if _, err := reloadAllSysctlConfiguration(); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to reload /etc/sysctl.d after writing %s", networkTuningDestPath).
			WithProperty(models.ErrPropertyResolution, []string{
				"Run: sudo sysctl --system",
			})
	}

	m.restoreFileMax()

	return m.verifyLiveValues()
}

// restoreFileMax writes back the pre-install fs.file-max value recorded in the
// sysctl backup that BackupSettings writes during 'kube cluster install'.
// Dropping a line from a .conf file and reloading does not reset a live kernel
// value, so without this the host stays pinned at the old value until reboot.
//
// This is best-effort and unverified: a missing backup, a backup without
// fs.file-max, or a failed Set only logs (Warn/Debug) and never fails Execute.
// Unlike tcp_keepalive_probes, there's no fixed "correct" fs.file-max to assert
// against — the right value is whatever this host's own backup says, which can
// legitimately be any number, including one that looks like the old override.
// A hard verification here would risk permanently failing the migration on a
// host where that's the genuinely correct value.
func (m *SysctlNetworkTuningMigration) restoreFileMax() {
	backupFile := path.Join(models.Paths().BackupDir, steps.SysCtlBackupFilename)

	value, ok, err := sysctl.ReadBackupValue(backupFile, fileMaxKey)
	if err != nil {
		logx.As().Warn().Err(err).Str("backup", backupFile).
			Msg("sysctl network tuning migration: failed to read the sysctl backup; leaving fs.file-max as-is")
		return
	}
	if !ok {
		logx.As().Debug().Str("backup", backupFile).
			Msg("sysctl network tuning migration: fs.file-max not recorded in the sysctl backup; leaving it as-is")
		return
	}

	if err := setSysctlValue(fileMaxKey, value); err != nil {
		logx.As().Warn().Err(err).Str("value", value).
			Msg("sysctl network tuning migration: failed to restore fs.file-max from the sysctl backup")
	}
}

// verifyLiveValues checks that tcp_keepalive_probes actually converged and
// returns an errx-decorated error with an operator hint if it did not.
//
// fs.file-max is deliberately not checked here — see restoreFileMax. Unlike
// fs.file-max, tcp_keepalive_probes has one fixed, host-independent correct
// value (the static template's "5"), so a positive assertion against it carries
// none of the false-failure risk a fs.file-max check would.
func (m *SysctlNetworkTuningMigration) verifyLiveValues() error {
	v, err := getSysctlValue(keepaliveProbesKey)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to read %s after reload", keepaliveProbesKey).
			WithProperty(models.ErrPropertyResolution, []string{
				"Re-run any solo-provisioner command to retry the migration.",
			})
	}
	if v != wantKeepaliveProbesValue {
		return errorx.IllegalState.New("%s is %q, expected %q", keepaliveProbesKey, v, wantKeepaliveProbesValue).
			WithProperty(models.ErrPropertyResolution, []string{
				"Re-run any solo-provisioner command to retry the migration.",
			})
	}

	return nil
}

// sysctlNetworkTuningPrevContentsKey stashes the pre-migration file bytes in the
// migration context so Rollback can restore them, per the Migration interface's
// stateless-implementation contract (state travels via Context, not struct fields).
const sysctlNetworkTuningPrevContentsKey = "sysctlNetworkTuningPrevContents"

// Rollback restores the previous file (if any was overwritten) and reloads it.
func (m *SysctlNetworkTuningMigration) Rollback(ctx context.Context, mctx *migration.Context) error {
	v, ok := mctx.Data.Get(sysctlNetworkTuningPrevContentsKey)
	if !ok {
		return nil // nothing was overwritten in Execute
	}
	prev, ok := v.([]byte)
	if !ok {
		return nil
	}

	logx.As().Warn().Str("file", networkTuningDestPath).
		Msg("Rolling back the network tuning sysctl file on this host")

	if err := os.WriteFile(networkTuningDestPath, prev, models.DefaultFilePerm); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to restore the previous %s during rollback", networkTuningDestPath)
	}

	if _, err := reloadAllSysctlConfiguration(); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to reload /etc/sysctl.d during rollback")
	}

	return nil
}
