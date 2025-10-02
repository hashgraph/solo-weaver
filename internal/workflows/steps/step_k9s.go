package steps

import (
	"context"
	"github.com/automa-saga/automa"
)

func InstallK9s() automa.Builder {
	return automa.NewStepBuilder("install-k9s", automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		return automa.StepSuccessReport("install-k9s"), nil
	}))
}
