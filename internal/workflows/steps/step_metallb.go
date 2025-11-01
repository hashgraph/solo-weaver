package steps

import (
	"context"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
	"golang.hedera.com/solo-provisioner/pkg/helm"
	"helm.sh/helm/v3/pkg/cli/values"
)

const (
	MetalLBNamespace = "metallb-system"
	MetalLBRelease   = "metallb"
	MetalLBChart     = "metallb/metallb"
	MetalLBVersion   = "v0.15.2"
	MetalLBRepo      = "https://metallb.github.io/metallb"
)

func SetupMetalLB() automa.Builder {
	return automa.NewWorkflowBuilder().WithId("setup-metallb").Steps(
		installMetalLB(),
		bashSteps.ConfigureMetalLB(),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up MetalLB")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup MetalLB")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "MetalLB setup successfully")
		})
}

func installMetalLB() automa.Builder {
	return automa.NewStepBuilder().WithId("install-metallb").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			l := logx.As()
			hm, err := helm.NewManager(helm.WithLogger(*l))
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			isInstalled, err := hm.IsInstalled(MetalLBRelease, MetalLBNamespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if isInstalled {
				meta[AlreadyInstalled] = "true"
				l.Info().Msg("MetalLB is already installed, skipping installation")
				return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
			}

			_, err = hm.AddRepo(MetalLBRelease, MetalLBRepo, helm.RepoAddOptions{})
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			_, err = hm.InstallChart(
				ctx,
				MetalLBRelease,
				MetalLBChart,
				MetalLBVersion,
				MetalLBNamespace,
				helm.InstallChartOptions{
					ValueOpts: &values.Options{
						Values: []string{"speaker.frr.enabled=false"},
					},
					CreateNamespace: true,
				},
			)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta[InstalledByThisStep] = "true"
			stp.State().Set(InstalledByThisStep, true)

			// TODO: replace with proper readiness check using k8s client
			time.Sleep(60 * time.Second) // wait for metallb to be ready

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			if stp.State().Bool(InstalledByThisStep) == false {
				return automa.StepSkippedReport(stp.Id())
			}

			l := logx.As()
			hm, err := helm.NewManager(helm.WithLogger(*l))
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			err = hm.UninstallChart(MetalLBRelease, MetalLBNamespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Installing MetalLB")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to install MetalLB")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "MetalLB installed successfully")
		})
}
