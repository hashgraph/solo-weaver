// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
)

// EnsureConsensusUpgradeDirStepId is the step ID for EnsureConsensusUpgradeDirStep.
const EnsureConsensusUpgradeDirStepId = "ensure-consensus-upgrade-dir"

// EnsureConsensusUpgradeDirStep makes the consensus-node upgrade staging directory
// writable by the weaver daemon through the hedera group. On a node that already
// exists (e.g. mainnet) the directory is typically hedera:hedera 0755, which the
// daemon's disk prerequisite probes reject. The CN recreates the staging dir on every
// upgrade, so the parent is given a default ACL (group rwx for hedera) that new dirs
// inherit; the current dir also gets at least 0775 plus setgid now, keeping any extra bits.
func EnsureConsensusUpgradeDirStep(upgradeDir string) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(EnsureConsensusUpgradeDirStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if err := ensureConsensusUpgradeDir(upgradeDir); err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errx.Decorate(err, reasons.PreconditionNotMet, upgradeDirHints(err, upgradeDir)...)))
			}
			return automa.SuccessReport(stp, automa.WithMetadata(map[string]string{"upgrade_dir": upgradeDir}))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Ensuring consensus upgrade directory is group-writable")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to prepare consensus upgrade directory")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Consensus upgrade directory ready")
		})
}

// upgradeDirHints returns operator-actionable remediation for a failed upgrade dir setup.
// The setfacl command is only suggested when it is installed; on minimal images the
// operator is pointed at the acl package first.
func upgradeDirHints(err error, upgradeDir string) []string {
	parent := filepath.Dir(upgradeDir)
	if aclUnsupported(err) {
		return []string{
			fmt.Sprintf("The filesystem holding %s does not support POSIX ACLs. Remount it with ACL support (e.g. the 'acl' mount option) or use a filesystem that supports them.", parent),
			fmt.Sprintf("Alternatively make the dir group-writable manually after every CN upgrade: sudo chmod 2775 %s", upgradeDir),
		}
	}
	setfacl := fmt.Sprintf("sudo setfacl -d -m g:%s:rwx %s", config.HederaGroupName(), parent)
	if _, lookErr := exec.LookPath("setfacl"); lookErr != nil {
		setfacl = "Install the acl package (e.g. sudo apt install acl, or sudo dnf install acl), then run: " + setfacl
	}
	return []string{
		fmt.Sprintf("sudo chown %s:%s %s && sudo chmod 2775 %s", config.HederaUserName(), config.HederaGroupName(), upgradeDir, upgradeDir),
		setfacl,
		"Re-run this command once the directory is group-writable.",
	}
}

func ensureConsensusUpgradeDir(upgradeDir string) error {
	uid, err := strconv.Atoi(config.HederaUserId())
	if err != nil {
		return errorx.IllegalState.Wrap(err, "invalid hedera user ID: %s", config.HederaUserId())
	}
	gid, err := strconv.Atoi(config.HederaGroupId())
	if err != nil {
		return errorx.IllegalState.Wrap(err, "invalid hedera group ID: %s", config.HederaGroupId())
	}

	parent := filepath.Dir(upgradeDir)
	if _, err := os.Stat(parent); os.IsNotExist(err) {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return errorx.ExternalError.Wrap(err, "failed to create %s", parent)
		}
		if err := os.Chown(parent, uid, gid); err != nil {
			return errorx.ExternalError.Wrap(err, "failed to chown %s", parent)
		}
	}

	// The CN deletes and recreates the staging dir on every upgrade with its own umask,
	// so a one-off chmod is lost. A default ACL on the parent makes each new dir
	// inherit group rwx for hedera.
	// Fail install if the ACL cannot be set: without it the CN's next recreation of
	// the dir drops group write and the daemon's startup probe breaks.
	if err := setDefaultGroupACL(parent, gid); err != nil {
		return err
	}

	if err := os.MkdirAll(upgradeDir, 0o775); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to create %s", upgradeDir)
	}
	if err := os.Chown(upgradeDir, uid, gid); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to chown %s", upgradeDir)
	}
	info, err := os.Stat(upgradeDir)
	if err != nil {
		return errorx.ExternalError.Wrap(err, "failed to stat %s", upgradeDir)
	}
	// The daemon's ownership probe requires at least 0775 (owner rwx, group rwx, other r-x).
	want := info.Mode().Perm() | 0o775
	if err := os.Chmod(upgradeDir, want|os.ModeSetgid); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to chmod %s", upgradeDir)
	}
	logx.As().Info().Str("dir", upgradeDir).Msg("Ensured consensus upgrade directory is hedera-group writable")
	return nil
}
