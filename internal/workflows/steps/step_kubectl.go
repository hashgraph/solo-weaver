package steps

import (
	"context"
	"github.com/automa-saga/automa"
)

func InstallKubectl() automa.Builder {
	return automa.NewStepBuilder().WithId("install-kubectl").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SuccessReport(stp)
		})
}
