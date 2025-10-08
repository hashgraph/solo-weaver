package steps

import (
	"context"
	"github.com/automa-saga/automa"
)

func ConfigureSysctlForKubernetes() automa.Builder {
	return automa.NewStepBuilder().WithId("configure-sysctl").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SuccessReport(stp)
		})
}
