package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
	"golang.hedera.com/solo-provisioner/pkg/software"
)

func SetupCiliumCLI() automa.Builder {
	return automa.NewWorkflowBuilder().WithId("setup-cilium-cli").Steps(
		installCiliumCLI(software.NewCiliumInstaller),
		configureCiliumCLI(software.NewCiliumInstaller),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up Cilium CLI")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup Cilium CLI")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Cilium CLI setup successfully")
		})
}

func installCiliumCLI(provider func(opts ...software.InstallerOption) (software.Software, error)) automa.Builder {
	return automa.NewStepBuilder().WithId("install-cilium-cli").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			installer, err := provider()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			err = installer.Download()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}
			err = installer.Extract()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}
			err = installer.Install()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}
			err = installer.Cleanup()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			return automa.SuccessReport(stp)
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SuccessReport(stp)
		})
}

func configureCiliumCLI(provider func(opts ...software.InstallerOption) (software.Software, error)) automa.Builder {
	return automa.NewStepBuilder().WithId("configure-cilium-cli").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			installer, err := provider()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			err = installer.Configure()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			return automa.SuccessReport(stp)
		})
}
