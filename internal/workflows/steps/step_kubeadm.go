package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/kube"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
)

const (
	// TODO retrieve from software package config
	KubernetesVersion = "1.33.4"
)

func SetupKubeadm() automa.Builder {
	return automa.NewWorkflowBuilder().WithId("setup-kubeadm").Steps(
		bashSteps.DownloadKubeadm(),
		bashSteps.InstallKubeadm(),
		bashSteps.TorchPriorKubeAdmConfiguration(), // we cannot write in pure Go because we need to run kubeadm binary
		bashSteps.DownloadKubeadmConfig(),
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
		configureKubeadmInit(),
		bashSteps.InitCluster(), // we cannot write in pure Go because we need to run kubadm binary
		configureKubeConfig(),
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

// configureKubeadmInit generates the kubeadm init configuration file
// It retrieves the machine IP, generates a kubeadm token, and gets the hostname
// It then renders the kubeadm-init.yaml template with the retrieved values
func configureKubeadmInit() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(ConfigureKubeadmInitStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			err := kube.ConfigureKubeadmInit(KubernetesVersion)
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(
						automa.StepExecutionError.Wrap(err, "failed to configure kubeadm init")))
			}

			return automa.SuccessReport(stp)
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Configuring kubeadm init")
			return ctx, nil
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepCompletion(ctx, stp, report, "kubeadm init configured successfully")
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepFailure(ctx, stp, report, "Failed to configure kubeadm init")
		})
}

// configureKubeConfig copies the kubeconfig file to the user's home directory and sets the ownership
// to the current user. This allows kubectl to be used without requiring root privileges.
func configureKubeConfig() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(ConfigureKubeletStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			err := kube.ConfigureKubeConfig()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(
						automa.StepExecutionError.Wrap(err, "failed to configure kubeconfig")))
			}

			return automa.SuccessReport(stp)
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Configuring kubeconfig for the cluster")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepFailure(ctx, stp, report, "Failed to configure kubeconfig for the cluster")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepCompletion(ctx, stp, report, "kubeconfig configured successfully for the cluster")
		})
}
