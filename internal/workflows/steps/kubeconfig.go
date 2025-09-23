package steps

import (
	"context"
	"github.com/automa-saga/automa"
)

func ConfigureKubeconfigForAdminUser() automa.Builder {
	return automa.NewStepBuilder("configure-kube-config", automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		return automa.StepSuccessReport("configure-kube-config"), nil
	}))
}
