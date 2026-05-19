// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"helm.sh/helm/v3/pkg/cli/values"
)

func DeployMetricsServer(valueOptions *values.Options) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("deploy-metrics-server").Steps(
		installMetricsServer(valueOptions),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Deploying Metrics ServerInfo")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to deploy Metrics ServerInfo")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Metrics ServerInfo deployed successfully")
		})
}

func installMetricsServer(valueOptions *values.Options) *automa.StepBuilder {
	if valueOptions == nil {
		valueOptions = &values.Options{
			Values: []string{
				"args={--kubelet-insecure-tls}",
				"apiService.insecureSkipTLSVerify=true",
			},
		}
	}

	spec := chartSpec("metrics-server")
	return automa.NewStepBuilder().WithId("enable-metrics-server").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			l := logx.As()
			hm, err := newHelmManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			isInstalled, err := hm.IsInstalled(spec.Release, spec.Namespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if isInstalled {
				meta[AlreadyInstalled] = "true"
				l.Info().Msg("Metrics server is already installed, skipping installation")
				return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
			}

			_, err = hm.AddRepo(spec.Release, spec.Repo, helm.RepoAddOptions{})
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			localChart, err := hm.PullAndVerify(ctx, chartDownloadsDir(), spec.Chart, spec.Version, spec.Algorithm, spec.Checksum)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			_, err = hm.InstallChart(
				ctx,
				spec.Release,
				localChart,
				"",
				spec.Namespace,
				helm.InstallChartOptions{
					ValueOpts:       valueOptions,
					CreateNamespace: true,
					Atomic:          true,
					Wait:            true,
					Timeout:         helm.DefaultTimeout, // 5 minutes
				},
			)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta[InstalledByThisStep] = "true"
			stp.State().Local().Set(InstalledByThisStep, true)

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			if v, _ := stp.State().Local().Bool(InstalledByThisStep); v == false {
				return automa.StepSkippedReport(stp.Id())
			}

			hm, err := newHelmManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			err = hm.UninstallChart(spec.Release, spec.Namespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			return automa.StepSuccessReport(stp.Id())
		})
}
