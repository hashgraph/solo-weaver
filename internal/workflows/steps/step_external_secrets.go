// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"helm.sh/helm/v3/pkg/cli/values"
)

const (
	ExternalSecretsNamespace     = "external-secrets"
	ExternalSecretsRelease       = "external-secrets"
	ExternalSecretsChart         = "external-secrets/external-secrets"
	ExternalSecretsVersion       = "0.20.2"
	ExternalSecretsRepo          = "https://charts.external-secrets.io"
	SetupExternalSecretsStepId   = "setup-external-secrets"
	InstallExternalSecretsStepId = "install-external-secrets"
	IsExternalSecretsReadyStepId = "is-external-secrets-ready"
)

// SetupExternalSecrets returns a workflow builder that sets up External Secrets Operator.
func SetupExternalSecrets() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId(SetupExternalSecretsStepId).Steps(
		installExternalSecrets(),
		isExternalSecretsReady(),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up External Secrets Operator")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup External Secrets Operator")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "External Secrets Operator setup successfully")
		})
}

func installExternalSecrets() automa.Builder {
	return automa.NewStepBuilder().WithId(InstallExternalSecretsStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			l := logx.As()
			hm, err := helm.NewManager(helm.WithLogger(*l))
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			isInstalled, err := hm.IsInstalled(ExternalSecretsRelease, ExternalSecretsNamespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if isInstalled {
				meta[AlreadyInstalled] = "true"
				l.Info().Msg("External Secrets Operator is already installed, skipping installation")
				return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
			}

			_, err = hm.AddRepo("external-secrets", ExternalSecretsRepo, helm.RepoAddOptions{})
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			helmValues := []string{
				"installCRDs=true",
				"webhook.port=9443",
			}

			_, err = hm.InstallChart(
				ctx,
				ExternalSecretsRelease,
				ExternalSecretsChart,
				ExternalSecretsVersion,
				ExternalSecretsNamespace,
				helm.InstallChartOptions{
					ValueOpts: &values.Options{
						Values: helmValues,
					},
					CreateNamespace: true,
					Atomic:          true,
					Wait:            true,
					Timeout:         helm.DefaultTimeout,
				},
			)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta[InstalledByThisStep] = "true"
			stp.State().Set(InstalledByThisStep, true)

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

			err = hm.UninstallChart(ExternalSecretsRelease, ExternalSecretsNamespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Installing External Secrets Operator")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to install External Secrets Operator")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "External Secrets Operator installed successfully")
		})
}

func isExternalSecretsReady() automa.Builder {
	return automa.NewStepBuilder().WithId(IsExternalSecretsReadyStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			k, err := kube.NewClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			// Wait for external-secrets pods to be ready
			err = k.WaitForResources(ctx, kube.KindPod, ExternalSecretsNamespace, kube.IsPodReady, 5*time.Minute, kube.WaitOptions{NamePrefix: "external-secrets"})
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta[IsReady] = "true"
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Verifying External Secrets Operator readiness")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "External Secrets Operator is not ready")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "External Secrets Operator is ready")
		})
}
