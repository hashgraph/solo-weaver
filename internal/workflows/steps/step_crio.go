package steps

import (
	"context"
	"github.com/automa-saga/automa"
)

func InstallCrio() automa.Builder {
	return automa.NewStepBuilder().WithId("install-crio").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SuccessReport(stp)
		})
}

func ConfigureCrio() automa.Builder {
	return automa.NewStepBuilder().WithId("configure-crio").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SuccessReport(stp)
		})
}

func EnableAndStartCrio() automa.Builder {
	return automa.NewStepBuilder().WithId("start-crio").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SuccessReport(stp)
		})
}
