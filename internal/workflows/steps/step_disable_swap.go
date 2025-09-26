package steps

import (
	"bufio"
	"context"
	"github.com/automa-saga/automa"
	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
	"os"
	"path"
	"strings"
)

const (
	DisableSwapStepId      = "disable-swap"
	BackupFstabStepId      = "backup-fstab"
	RestoreFstabStepId     = "restore-fstab"
	CommentSwapFstabStepId = "comment-swap-fstab"
	SwapOffStepId          = "swap-off"
	SwapOnStepId           = "swap-on"
)

// We are defining these package private variables to help with mocking for unit tests.
// These also helps to avoid hardcoding values
var (
	etcBackupDir    = path.Join(core.BackupDir, "etc")
	etcDir          = "/etc"
	fstabPath       = path.Join(etcDir, "fstab")
	fstabBackupPath = path.Join(etcBackupDir, "fstab")
)

// BackupFstabFile backs up the /etc/fstab file to the backup directory
// This does not have rollback behaviour, rather call restoreFstabFile to restore the file
// This does not overwrite existing backup (to preserve previous state if multiple calls are made)
func backupFstabFile() automa.Builder {
	return automa.NewStepBuilder(BackupFstabStepId, automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		fm, err := fsx.NewManager()
		if err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to create file system manager")
		}

		if err := os.MkdirAll(etcBackupDir, 0755); err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to create backup dir")
		}

		err = fm.CopyFile(fstabPath, etcBackupDir, false)
		if err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to backup fstab")
		}

		return automa.StepSuccessReport(BackupFstabStepId), nil
	}))
}

// RestoreFstabFile restores the /etc/fstab file from the backup directory
// This does not have rollback behaviour, rather call backupFstabFile to re-backup the file if needed
func restoreFstabFile() automa.Builder {
	return automa.NewStepBuilder(RestoreFstabStepId, automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		fm, err := fsx.NewManager()
		if err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to create file system manager")
		}

		err = fm.CopyFile(fstabBackupPath, etcDir, true)
		if err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to recover fstab from backup")
		}

		return automa.StepSuccessReport(RestoreFstabStepId), nil
	}))
}

// CommentSwapSettingsStep comments out any swap entries in /etc/fstab to disable swap on reboot
// It finds lines in /etc/fstab containing the word "swap" and comments them out by prefixing with #
// This does not have rollback behaviour, rather call restoreFstabFile to restore the original file
func commentSwapSettings() automa.Builder {
	return automa.NewStepBuilder(CommentSwapFstabStepId, automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		// Read original file
		input, err := os.ReadFile(fstabPath)
		if err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to read fstab")
		}

		info, err := os.Stat(fstabPath)
		if err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to stat fstab")
		}

		// Process lines
		var output []string
		scanner := bufio.NewScanner(strings.NewReader(string(input)))
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "#") {
				fields := strings.Fields(line)
				for _, field := range fields {
					if field == "swap" {
						line = "#" + line
						break
					}
				}
			}
			output = append(output, line)
		}

		if err = scanner.Err(); err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to scan fstab")
		}

		// Write modified file
		err = os.WriteFile(fstabPath, []byte(strings.Join(output, "\n")+"\n"), info.Mode())
		if err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to write fstab")
		}

		return automa.StepSuccessReport(CommentSwapFstabStepId), nil
	}))
}

// SwapOffStep disables swap on the system
// This does not have rollback behaviour, rather call swapOn to re-enable swap if needed
func swapOff() automa.Builder {
	return automa.NewStepBuilder(SwapOffStepId, automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		if err := RunCmd("swapoff", "-a"); err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to disable swap")
		}
		return automa.StepSuccessReport(SwapOffStepId), nil
	}))
}

// SwapOnStep enables swap on the system
// This does not have rollback behaviour, rather call swapOff to disable swap if needed
func swapOn() automa.Builder {
	return automa.NewStepBuilder(SwapOnStepId, automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		if err := RunCmd("swapon", "-a"); err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to enable swap")
		}

		return automa.StepSuccessReport(SwapOnStepId), nil
	}))
}

// DisableSwapWorkflow is a workflow that disables swap on the system, which is needed by Kubernetes
// It consists of the following steps:
// 1. BackupFstabFile - backs up the /etc/fstab file to the backup directory
// 2. CommentSwapSettings - comments out any swap entries in /etc/fstab to disable swap on reboot
// 3. SwapOff - disables swap on the system
func disableSwapWorkflow() automa.Builder {
	return automa.NewWorkFlowBuilder(DisableSwapStepId+"_execute_wf").Steps(
		backupFstabFile(),
		commentSwapSettings(),
		swapOff(),
	)
}

// RestoreSwapWorkflow is a workflow that restores swap on the system
// It consists of the following steps:
// 1. RestoreFstabFile - restores the /etc/fstab file from the backup directory
// 2. SwapOn - enables swap on the system
func restoreSwapWorkflow() automa.Builder {
	return automa.NewWorkFlowBuilder(DisableSwapStepId+"_rollback_wf").Steps(
		restoreFstabFile(),
		swapOn(),
	)
}

// DisableSwap disables swap on the system
// On execute, it runs the disable swap workflow
// On rollback, it runs the restore swap workflow
// It returns a composite workflow that includes both the disable and restore workflows with multiple steps in each
func DisableSwap() automa.Builder {
	disableWorkflow := disableSwapWorkflow()
	restoreWorkflow := restoreSwapWorkflow()

	// wrap the disable swap and restore swap steps in a single step to allow for easier rollback
	// on execute, run the disable swap workflow
	// on rollback, run the restore swap workflow
	return automa.NewStepBuilder(DisableSwapStepId, automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		dw, err := disableWorkflow.Build()
		if err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to build disable swap workflow")
		}

		return dw.Execute(ctx)
	}), automa.WithOnRollback(func(ctx context.Context) (*automa.Report, error) {
		rw, err := restoreWorkflow.Build()
		if err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to build restore swap workflow")
		}

		return rw.Execute(ctx)
	}))
}
