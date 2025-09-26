package steps

import (
	"context"
	"github.com/automa-saga/automa"
)

func InstallHelm() automa.Builder {
	return automa.NewStepBuilder("install-helm", automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		return automa.StepSuccessReport("install-helm"), nil
	}))
}
