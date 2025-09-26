package steps

import (
	"context"
	"github.com/automa-saga/automa"
)

func InstallCrio() automa.Builder {
	return automa.NewStepBuilder("install-crio", automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		return automa.StepSuccessReport("install-crio"), nil
	}))
}

func ConfigureCrio() automa.Builder {
	return automa.NewStepBuilder("configure-crio", automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		return automa.StepSuccessReport("configure-crio"), nil
	}))
}

func EnableAndStartCrio() automa.Builder {
	return automa.NewStepBuilder("start-crio", automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		return automa.StepSuccessReport("start-crio"), nil
	}))
}
