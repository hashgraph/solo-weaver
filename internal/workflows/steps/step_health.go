package steps

import (
	"context"
	"github.com/automa-saga/automa"
)

func CheckClusterHealth() automa.Builder {
	return automa.NewStepBuilder("check-cluster-health", automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		return automa.StepSuccessReport("check-cluster-health"), nil
	}))
}
