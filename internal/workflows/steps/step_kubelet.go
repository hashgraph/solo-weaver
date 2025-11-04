package steps

import (
	"context"
	"fmt"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
	"golang.hedera.com/solo-provisioner/pkg/software"
)

func SetupKubelet() automa.Builder {
	return automa.NewWorkflowBuilder().WithId("setup-kubelet").
		Steps(
			installKubelet(software.NewKubeletInstaller),
			configureKubelet(software.NewKubeletInstaller),
			SetupSystemdService("kubelet"),
		).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up kubelet")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup kubelet")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "kubelet setup successfully")
		})
}

func installKubelet(provider func(opts ...software.InstallerOption) (software.Software, error)) automa.Builder {
	return automa.NewStepBuilder().WithId("install-kubelet").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Installing kubelet")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to install kubelet")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "kubelet installed successfully")
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
				return automa.SkippedReport(stp, automa.WithDetail("kubelet is already installed"), automa.WithMetadata(meta))
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
				return automa.SkippedReport(stp, automa.WithDetail("kubelet was not installed by this step, skipping rollback"))
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

func configureKubelet(provider func(opts ...software.InstallerOption) (software.Software, error)) automa.Builder {
	return automa.NewStepBuilder().WithId("configure-kubelet").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Configuring kubelet")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to configure kubelet")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "kubelet configured successfully")
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
				return automa.SkippedReport(stp, automa.WithDetail("kubelet is already configured"), automa.WithMetadata(meta))
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
			a := stp.State().Items()
			fmt.Println(a)
			configuredByThisStep := stp.State().Bool(ConfiguredByThisStep)
			if !configuredByThisStep {
				return automa.SkippedReport(stp, automa.WithDetail("kubelet was not configured by this step, skipping rollback"))
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
