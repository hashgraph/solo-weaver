package steps

import (
	"context"

	"github.com/automa-saga/automa"
	osx "golang.hedera.com/solo-provisioner/pkg/os"
)

const (
	DisableSwapStepId = "disable-swap"
)

// DisableSwap disables swap on the system
// On execute, it runs the swapoff and ensures fstab is updated to prevent swap from being re-enabled on reboot
// On rollback, it runs the swapon and ensures fstab is updated to re-enable swap on reboot
func DisableSwap() automa.Builder {
	return automa.NewStepBuilder(DisableSwapStepId, automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		err := osx.DisableSwap()
		if err != nil {
			return nil, automa.StepExecutionError.Wrap(err, "failed to disable swap")
		}
		return automa.StepSuccessReport(DisableSwapStepId), nil
	}), automa.WithOnRollback(func(ctx context.Context) (*automa.Report, error) {
		err := osx.EnableSwap()
		if err != nil {
			return nil, automa.StepExecutionError.Wrap(err, "failed to enable swap")
		}
		return automa.StepSuccessReport(DisableSwapStepId), nil
	}))
}
