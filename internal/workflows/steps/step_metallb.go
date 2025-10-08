package steps

import (
	"context"
	"github.com/automa-saga/automa"
)

func InstallMetalLB() automa.Builder {
	return automa.NewStepBuilder().WithId("install-metallb").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SuccessReport(stp)
		})
}

func DeployMetallbConfig() automa.Builder {
	return automa.NewStepBuilder().WithId("deploy-metallb-config").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SuccessReport(stp)
		})
}
