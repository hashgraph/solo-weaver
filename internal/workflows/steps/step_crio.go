package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/pkg/software"
)

func SetupCrio() automa.Builder {
	crioInstaller, err := software.NewCrioInstaller()
	if err != nil {
		panic(err)
	}

	return automa.NewWorkflowBuilder().WithId("setup-crio").Steps(
		installCrio(crioInstaller),
		configureCrio(crioInstaller),
		enableAndStartCrio(crioInstaller),
	)
}

func installCrio(installer software.Software) automa.Builder {
	return automa.NewStepBuilder().WithId("install-crio").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			err := installer.Download()
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

func configureCrio(installer software.Software) automa.Builder {
	return automa.NewStepBuilder().WithId("configure-crio").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			err := installer.Configure()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			return automa.SuccessReport(stp)
		})
}

func enableAndStartCrio(installer software.Software) automa.Builder {
	return automa.NewStepBuilder().WithId("start-crio").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			err := installer.Configure()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			return automa.SuccessReport(stp)
		})
}
