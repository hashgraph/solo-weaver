// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

func SetupK9s(sm state.Manager) *automa.WorkflowBuilder {

	return automa.NewWorkflowBuilder().WithId("setup-k9s").
		Steps(
			installK9s(software.NewK9sInstaller, sm),
			configureK9s(software.NewK9sInstaller, sm),
		).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up K9s")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup K9s")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "K9s setup successfully")
		})
}

func installK9s(provider func(opts ...software.InstallerOption) (software.Software, error), sm state.Manager) automa.Builder {
	return automa.NewStepBuilder().WithId("install-k9s").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepDetail(ctx, stp, "installing K9s...")
			return ctx, nil
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			installer, err := provider(software.WithStateManager(sm))
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			logx.As().Info().Str("step_id", stp.Id()).Str("software", installer.GetSoftwareName()).Str("version", installer.Version()).Msgf("%s version: %s", installer.GetSoftwareName(), installer.Version())

			// Prepare metadata for reporting
			meta := map[string]string{}

			installed, err := installer.IsInstalled()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			if installed {
				meta[AlreadyInstalled] = "true"
				return automa.SkippedReport(stp, automa.WithDetail("K9s is already installed"), automa.WithMetadata(meta))
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
			stp.State().Local().Set(InstalledByThisStep, true)

			err = installer.Cleanup()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			meta[CleanedUpByThisStep] = "true"

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			var installedByThisStep bool
			if v, ok := stp.State().Local().Bool(InstalledByThisStep); ok {
				installedByThisStep = v
			}

			if !installedByThisStep {
				return automa.SkippedReport(stp, automa.WithDetail("K9s was not installed by this step, skipping rollback"))
			}

			installer, err := provider(software.WithStateManager(sm))
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

func configureK9s(provider func(opts ...software.InstallerOption) (software.Software, error), sm state.Manager) automa.Builder {
	return automa.NewStepBuilder().WithId("configure-k9s").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepDetail(ctx, stp, "configuring K9s...")
			return ctx, nil
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			installer, err := provider(software.WithStateManager(sm))
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
				return automa.SkippedReport(stp, automa.WithDetail("K9s is already configured"), automa.WithMetadata(meta))
			}

			err = installer.Configure()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}

			meta[ConfiguredByThisStep] = "true"
			stp.State().Local().Set(ConfiguredByThisStep, true)

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			var configuredByThisStep bool
			if v, ok := stp.State().Local().Bool(ConfiguredByThisStep); ok {
				configuredByThisStep = v
			}

			if !configuredByThisStep {
				return automa.SkippedReport(stp, automa.WithDetail("K9s was not configured by this step, skipping rollback"))
			}

			installer, err := provider(software.WithStateManager(sm))
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
