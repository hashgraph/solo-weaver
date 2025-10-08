package steps

import (
	"context"
	"github.com/automa-saga/automa"
)

func InstallKubelet() automa.Builder {
	return automa.NewStepBuilder().WithId("install-kubelet").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SuccessReport(stp)
		})
}

func ConfigureKubelet() automa.Builder {
	return automa.NewStepBuilder().WithId("configure-kubelet").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SuccessReport(stp)
		})
}

func EnableAndStartKubelet() automa.Builder {
	return automa.NewStepBuilder().WithId("start-kubelet").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SuccessReport(stp)
		})
}
