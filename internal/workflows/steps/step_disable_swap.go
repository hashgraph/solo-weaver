// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"

	"github.com/automa-saga/errx"
	"github.com/joomcode/errorx"

	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/reasons"

	"github.com/automa-saga/automa"
	osx "github.com/hashgraph/solo-weaver/pkg/os"
)

const (
	DisableSwapStepId = "disable-swap"
)

// swapInUse, disableSwap and enableSwap are package vars so tests can stub the host.
var (
	swapInUse   = osx.SwapInUse
	disableSwap = osx.DisableSwap
	enableSwap  = osx.EnableSwap
)

// DisableSwap disables swap on the system
// On execute, it runs the swapoff and ensures fstab is updated to prevent swap from being re-enabled on reboot
// On rollback, it runs the swapon and ensures fstab is updated to re-enable swap on reboot,
// but only when this step turned swap off: swap that was already off is outside its rollback scope.
func DisableSwap() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(DisableSwapStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			inUse, err := swapInUse()
			if err != nil && errorx.IsOfType(err, osx.ErrSwapDeviceNotFound) {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(
					errorx.ExternalError.Wrap(err, "failed to check whether swap is in use"),
					reasons.FileMissing,
					"An uncommented swap entry in /etc/fstab points at a device or file that does not exist: comment it out, then re-run")))
			}
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(
					errorx.ExternalError.Wrap(err, "failed to check whether swap is in use"),
					reasons.PreconditionNotMet,
					"Check that /proc/swaps and /etc/fstab are readable and that systemd answers 'systemctl list-units --type=swap'")))
			}
			if !inUse {
				return automa.SkippedReport(stp, automa.WithDetail("Swap is already disabled"))
			}

			// Swap is in use, so whatever DisableSwap touches is this run's to undo,
			// even when it fails midway.
			stp.State().Local().Set(SwapDisabledByThisStep, true)
			if err := disableSwap(); err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(
					automa.StepExecutionError.Wrap(err, "failed to disable swap"),
					ReasonHostCommandFailed,
					"Check 'swapon --show' and 'free -h': swapoff fails when there is not enough free memory to move swapped pages back",
					"Turn swap off by hand with 'sudo swapoff -a', then re-run")))
			}
			return automa.SuccessReport(stp)

		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			if disabled, _ := stp.State().Local().Bool(SwapDisabledByThisStep); !disabled {
				return automa.SkippedReport(stp, automa.WithDetail("Swap was not disabled by this step, skipping rollback"))
			}

			err := enableSwap()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(automa.StepExecutionError.Wrap(err, "failed to enable swap on rollback")))
			}

			return automa.SuccessReport(stp)
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Disabling swap")
			return ctx, nil

		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepFailure(ctx, stp, report, "Failed to disable swap")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepCompletion(ctx, stp, report, "Swap disabled successfully")
		})
}
