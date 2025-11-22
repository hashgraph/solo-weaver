package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-weaver/internal/workflows/notify"
	"golang.hedera.com/solo-weaver/pkg/software"
)

func SetupCrio() automa.Builder {

	return automa.NewWorkflowBuilder().WithId("setup-crio").
		Steps(
			installCrio(software.NewCrioInstaller),
			configureCrio(software.NewCrioInstaller),
		).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up cri-o")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup cri-o")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Cri-o setup successfully")
		})
}

func installCrio(provider func(opts ...software.InstallerOption) (software.Software, error)) automa.Builder {
	return automa.NewStepBuilder().WithId("install-crio").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Installing cri-o")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to install cri-o")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Cri-o installed successfully")

			// Record installation state if this step installed the software
			if stp.State().Bool(InstalledByThisStep) {
				if installer, err := provider(); err == nil {
					recordInstallState(installer)
				}
			}
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
				return automa.SkippedReport(stp, automa.WithDetail("cri-o is already installed"), automa.WithMetadata(meta))
			}

			err = installer.Download()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			meta[DownloadedByThisStep] = "true"

			err = installer.Extract()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			meta[ExtractedByThisStep] = "true"

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
				return automa.SkippedReport(stp, automa.WithDetail("cri-o was not installed by this step, skipping rollback"))
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

			// Remove installation state during rollback
			removeInstallState(installer)

			return automa.SuccessReport(stp)
		})
}

func configureCrio(provider func(opts ...software.InstallerOption) (software.Software, error)) automa.Builder {
	return automa.NewStepBuilder().WithId("configure-crio").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Configuring cri-o")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to configure cri-o")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Cri-o configured successfully")

			// Record configuration state if this step configured the software
			if stp.State().Bool(ConfiguredByThisStep) {
				if installer, err := provider(); err == nil {
					recordConfigureState(installer)
				}
			}
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
				return automa.SkippedReport(stp, automa.WithDetail("cri-o is already configured"), automa.WithMetadata(meta))
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
				return automa.SkippedReport(stp, automa.WithDetail("cri-o was not configured by this step, skipping rollback"))
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

			// Remove configuration state during rollback
			removeConfigureState(installer)

			return automa.SuccessReport(stp)
		})
}
