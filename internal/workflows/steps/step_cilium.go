package steps

import (
	"context"
	"github.com/automa-saga/automa"
)

func InstallCilium() automa.Builder {
	return automa.NewStepBuilder().WithId("install-cilium").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SuccessReport(stp)
		})
}

func ConfigureCilium() automa.Builder {
	return automa.NewStepBuilder().WithId("configure-cilium").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SuccessReport(stp)
		})
}

func EnableAndStartCilium() automa.Builder {
	return automa.NewStepBuilder().WithId("start-cilium").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SuccessReport(stp)
		})
}
