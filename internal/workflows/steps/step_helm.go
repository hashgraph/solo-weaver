package steps

import (
	"context"
	"github.com/automa-saga/automa"
)

func InstallHelm() automa.Builder {
	return automa.NewStepBuilder().WithId("install-helm").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SuccessReport(stp)
		})
}
