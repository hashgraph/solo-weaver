package steps

import (
	"context"
	"github.com/automa-saga/automa"
)

func ConfigureSysctlForKubernetes() automa.Builder {
	return automa.NewStepBuilder("configure-sysctl", automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		return automa.StepSuccessReport("configure-sysctl"), nil
	}))
}
