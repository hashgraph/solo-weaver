package steps

import (
	"context"
	"github.com/automa-saga/automa"
)

func CheckClusterHealth() automa.Builder {
	return automa.NewStepBuilder().WithId("check-cluster-health").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SuccessReport(stp)
		})
}
