// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"strings"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"helm.sh/helm/v3/pkg/cli/values"
)

const (
	MetricsServerNamespace    = "kube-system"
	MetricsServerRelease      = "metrics-server"
	MetricsServerChartName    = "metrics-server/metrics-server"
	MetricsServerChartVersion = "3.13.0"
	MetricsServerChartRepo    = "https://kubernetes-sigs.github.io/metrics-server"
)

func DeployMetricsServer(valueOptions *values.Options) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("deploy-metrics-server").Steps(
		installMetricsServer(valueOptions),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Deploying Metrics Server")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to deploy Metrics Server")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Metrics Server deployed successfully")
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

	return automa.NewStepBuilder().WithId("enable-metrics-server").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			l := logx.As()
			hm, err := helm.NewManager(helm.WithLogger(*l))
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			isInstalled, err := hm.IsInstalled(MetricsServerRelease, MetricsServerNamespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if isInstalled {
				meta[AlreadyInstalled] = "true"
				l.Info().Msg("Metrics server is already installed, skipping installation")
				return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
			}

			_, err = hm.AddRepo(MetricsServerRelease, MetricsServerChartRepo, helm.RepoAddOptions{})
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// if chartVersion doesn't start with "v", prepend it
			chartVersion := MetricsServerChartVersion
			if !strings.HasPrefix(chartVersion, "v") {
				chartVersion = "v" + chartVersion
			}

			_, err = hm.InstallChart(
				ctx,
				MetricsServerRelease,
				MetricsServerChartName,
				chartVersion,
				MetricsServerNamespace,
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

			err = hm.UninstallChart(MetricsServerRelease, MetricsServerNamespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			return automa.StepSuccessReport(stp.Id())
		})
}
