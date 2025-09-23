package steps

import (
	"context"
	"github.com/automa-saga/automa"
)

func InstallCilium() automa.Builder {
	return automa.NewStepBuilder("install-cilium", automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		return automa.StepSuccessReport("install-cilium"), nil
	}))
}

func ConfigureCilium() automa.Builder {
	return automa.NewStepBuilder("configure-cilium", automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		return automa.StepSuccessReport("configure-cilium"), nil
	}))
}

func EnableAndStartCilium() automa.Builder {
	return automa.NewStepBuilder("start-cilium", automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		return automa.StepSuccessReport("start-cilium"), nil
	}))
}
