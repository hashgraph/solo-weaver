package steps

import (
	"context"
	"fmt"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/automa/automa_steps"
	"github.com/automa-saga/logx"
	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/internal/kube"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
	"golang.hedera.com/solo-provisioner/pkg/software"
)

const kubectlGetNodesCmd = "/usr/local/bin/kubectl get nodes"

func SetupKubeadm() automa.Builder {
	return automa.NewWorkflowBuilder().WithId("setup-kubeadm").
		Steps(
			installKubeadm(software.NewKubeadmInstaller),
			configureKubeadm(software.NewKubeadmInstaller),
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

func installKubeadm(provider func(opts ...software.InstallerOption) (software.Software, error)) automa.Builder {
	return automa.NewStepBuilder().WithId("install-kubeadm").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Installing kubeadm")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to install kubeadm")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "kubeadm installed successfully")
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			installer, err := provider()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			// Prepare metadata for reporting
			meta := map[string]string{}

			installed, err := installer.IsInstalled()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			if installed {
				meta[AlreadyInstalled] = "true"
				return automa.SkippedReport(stp, automa.WithDetail("kubeadm is already installed"), automa.WithMetadata(meta))
			}

			err = installer.Download()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			meta[DownloadedByThisStep] = "true"

			err = installer.Install()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			meta[InstalledByThisStep] = "true"
			stp.State().Set(InstalledByThisStep, true)

			err = installer.Cleanup()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			meta[CleanedUpByThisStep] = "true"

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			installedByThisStep := stp.State().Bool(InstalledByThisStep)
			if !installedByThisStep {
				return automa.SkippedReport(stp, automa.WithDetail("kubeadm was not installed by this step, skipping rollback"))
			}

			installer, err := provider()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			err = installer.Uninstall()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			return automa.SuccessReport(stp)
		})
}

func configureKubeadm(provider func(opts ...software.InstallerOption) (software.Software, error)) automa.Builder {
	return automa.NewStepBuilder().WithId("configure-kubeadm").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Configuring kubeadm")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to configure kubeadm")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "kubeadm configured successfully")
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			installer, err := provider()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			// Prepare metadata for reporting
			meta := map[string]string{}

			configured, err := installer.IsConfigured()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			if configured {
				meta[AlreadyConfigured] = "true"
				return automa.SkippedReport(stp, automa.WithDetail("kubeadm is already configured"), automa.WithMetadata(meta))
			}

			err = installer.Configure()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}

			meta[ConfiguredByThisStep] = "true"
			stp.State().Set(ConfiguredByThisStep, true)

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			configuredByThisStep := stp.State().Bool(ConfiguredByThisStep)
			if !configuredByThisStep {
				return automa.SkippedReport(stp, automa.WithDetail("kubeadm was not configured by this step, skipping rollback"))
			}

			installer, err := provider()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			err = installer.RemoveConfiguration()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			return automa.SuccessReport(stp)
		})
}

// InitializeCluster checks cluster status and performs initialization only if needed
func InitializeCluster() automa.Builder {
	return automa.NewStepBuilder().WithId("init-cluster").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Initializing Kubernetes cluster")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to initialize Kubernetes cluster")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Kubernetes cluster initialized successfully")
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			// Check if cluster is already initialized
			scripts := []string{kubectlGetNodesCmd}
			output, err := automa_steps.RunBashScript(scripts, "")

			if err == nil && output != "" {
				// Cluster is already initialized, skip all initialization steps
				return automa.SkippedReport(stp,
					automa.WithDetail("Kubernetes cluster is already initialized"))
			}

			// Cluster not initialized, proceed with full initialization

			// Step 1: Pull kubeadm images
			logx.As().Info().Msg("Pulling kubeadm images, this may take a while...")
			pullImageCmd := []string{
				fmt.Sprintf("sudo %s/kubeadm config images pull --config %s/etc/provisioner/kubeadm-init.yaml", core.Paths().SandboxBinDir, core.Paths().SandboxDir),
			}

			_, err = automa_steps.RunBashScript(pullImageCmd, "")
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(fmt.Errorf("failed to pull kubeadm images: %w", err)))
			}
			logx.As().Info().Msg("Kubeadm images pulled successfully.")

			// Step 2: Initialize cluster with kubeadm
			initCmd := []string{
				fmt.Sprintf("sudo %s/kubeadm init --upload-certs --config %s/etc/provisioner/kubeadm-init.yaml", core.Paths().SandboxBinDir, core.Paths().SandboxDir),
			}

			_, err = automa_steps.RunBashScript(initCmd, "")
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(fmt.Errorf("failed to initialize cluster with kubeadm: %w", err)))
			}

			// Step 3: Configure kubeconfig
			kubeConfigManager, err := kube.NewKubeConfigManager()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(fmt.Errorf("failed to create kubeconfig manager: %w", err)))
			}
			err = kubeConfigManager.Configure()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(fmt.Errorf("failed to configure kubeconfig: %w", err)))
			}

			return automa.SuccessReport(stp, automa.WithDetail("Cluster initialized successfully"))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			scripts := []string{
				fmt.Sprintf("sudo %s/kubeadm reset --force --cri-socket unix:///opt/provisioner/sandbox/var/run/crio/crio.sock", core.Paths().SandboxBinDir),
			}

			_, err := automa_steps.RunBashScript(scripts, "")
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			return automa.SuccessReport(stp)
		})
}
