package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
)

func SetupKubeadm() automa.Builder {
	return automa.NewWorkflowBuilder().WithId("setup-kubeadm").Steps(
		bashSteps.DownloadKubeadm(),
		bashSteps.InstallKubeadm(),
		bashSteps.TorchPriorKubeAdmConfiguration(),
		bashSteps.DownloadKubeadmConfig(),
		bashSteps.ConfigureKubeadm(), // needed for kubelet
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up kubeadm")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup kubeadm")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "kubeadm setup successfully")
		})
}

func InitCluster() automa.Builder {
	return automa.NewWorkflowBuilder().WithId("init-cluster").Steps(
		bashSteps.InitCluster(),
		bashSteps.ConfigureKubeConfig(),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Initializing Kubernetes cluster")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to initialize Kubernetes cluster")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Kubernetes cluster initialized successfully")
		})
}
