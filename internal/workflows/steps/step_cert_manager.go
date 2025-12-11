package steps

import (
	"context"

	"github.com/automa-saga/automa"
)

func DeployCertManager() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("enable-metrics-server").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		})

	// Placeholder implementation
}
