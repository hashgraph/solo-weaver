package steps

import (
	"context"
	"github.com/automa-saga/automa"
)

func ConfigureKubeconfigForAdminUser() automa.Builder {
	return automa.NewStepBuilder().WithId("configure-kube-config").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SuccessReport(stp)
		})
}
