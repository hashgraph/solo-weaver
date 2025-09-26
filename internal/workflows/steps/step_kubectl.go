package steps

import (
	"context"
	"github.com/automa-saga/automa"
)

func InstallKubectl() automa.Builder {
	return automa.NewStepBuilder("install-kubectl", automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		return automa.StepSuccessReport("install-kubectl"), nil
	}))
}
