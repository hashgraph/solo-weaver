package steps

import (
	"context"
	"github.com/automa-saga/automa"
	"github.com/joomcode/errorx"
	"os/exec"
)

// DisableSwap disables swap on the system
// Essentially this is equivalent of running the `swapoff -a` command.
func DisableSwap() automa.Builder {
	return automa.NewStepBuilder("disable-swap", automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		// Disable swap
		cmd := exec.Command("swapoff", "-a")
		if err := cmd.Run(); err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to disable swap")
		}

		return automa.StepSuccessReport("disable-swap"), nil
	}))
}
