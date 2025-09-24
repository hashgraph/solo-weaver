package steps

import (
	"context"
	"github.com/automa-saga/automa"
)

func InstallKubeadm() automa.Builder {
	return automa.NewStepBuilder("install-kubeadm", automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		return automa.StepSuccessReport("install-kubeadm"), nil
	}))
}

func ConfigureKubeadm() automa.Builder {
	return automa.NewStepBuilder("configure-kubeadm", automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		return automa.StepSuccessReport("configure-kubeadm"), nil
	}))
}

func EnableAndStartKubeadm() automa.Builder {
	return automa.NewStepBuilder("start-kubeadm", automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		return automa.StepSuccessReport("start-kubeadm"), nil
	}))
}
