package steps

import (
	"context"
	"github.com/automa-saga/automa"
)

func HelmInstallMetallb() automa.Builder {
	return automa.NewStepBuilder("install-metallb", automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		return automa.StepSuccessReport("install-metallb"), nil
	}))
}

func DeployMetallbConfig() automa.Builder {
	return automa.NewStepBuilder("deploy-metallb-config", automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		return automa.StepSuccessReport("deploy-metallb-config"), nil
	}))
}
