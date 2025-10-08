package steps

import (
	"context"
	"github.com/automa-saga/automa"
)

func InstallK9s() automa.Builder {
	return automa.NewStepBuilder().WithId("install-k9s").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SuccessReport(stp)
		})
}
