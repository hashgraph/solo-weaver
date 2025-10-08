package steps

import (
	"context"
	"github.com/automa-saga/automa"
)

func InstallKubeadm() automa.Builder {
	return automa.NewStepBuilder().WithId("install-kubeadm").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SuccessReport(stp)
		})
}

func ConfigureKubeadm() automa.Builder {
	return automa.NewStepBuilder().WithId("configure-kubeadm").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SuccessReport(stp)
		})
}

func EnableAndStartKubeadm() automa.Builder {
	return automa.NewStepBuilder().WithId("start-kubeadm").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SuccessReport(stp)
		})
}
