package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/pkg/software"
)

func SetupK9s() automa.Builder {

	return automa.NewWorkflowBuilder().WithId("setup-k9s").Steps(
		installK9s(software.NewK9sInstaller),
		configureK9s(software.NewK9sInstaller),
	)
}

func installK9s(provider func(opts ...software.InstallerOption) (software.Software, error)) automa.Builder {
	return automa.NewStepBuilder().WithId("install-k9s").
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

			return automa.SuccessReport(stp)
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SuccessReport(stp)
		})
}

func configureK9s(provider func(opts ...software.InstallerOption) (software.Software, error)) automa.Builder {
	return automa.NewStepBuilder().WithId("configure-k9s").
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
