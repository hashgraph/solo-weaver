package steps

import (
	"context"
	"github.com/automa-saga/automa"
)

func InstallKubelet() automa.Builder {
	return automa.NewStepBuilder("install-kubelet", automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		return automa.StepSuccessReport("install-kubelet"), nil
	}))
}

func ConfigureKubelet() automa.Builder {
	return automa.NewStepBuilder("configure-kubelet", automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		return automa.StepSuccessReport("configure-kubelet"), nil
	}))
}

func EnableAndStartKubelet() automa.Builder {
	return automa.NewStepBuilder("start-kublet", automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		return automa.StepSuccessReport("start-kubelet"), nil
	}))
}
