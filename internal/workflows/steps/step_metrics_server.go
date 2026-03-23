// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"strings"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/deps"
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

	return automa.NewStepBuilder().WithId("enable-metrics-server").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			l := logx.As()
			hm, err := helm.NewManager(helm.WithLogger(*l))
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			isInstalled, err := hm.IsInstalled(deps.METRICS_SERVER_RELEASE, deps.METRICS_SERVER_NAMESPACE)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if isInstalled {
				meta[AlreadyInstalled] = "true"
				l.Info().Msg("Metrics server is already installed, skipping installation")
				return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
			}

			_, err = hm.AddRepo(deps.METRICS_SERVER_RELEASE, deps.METRICS_SERVER_REPO, helm.RepoAddOptions{})
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// if chartVersion doesn't start with "v", prepend it
			chartVersion := deps.METRICS_SERVER_VERSION
			if !strings.HasPrefix(chartVersion, "v") {
				chartVersion = "v" + chartVersion
			}

			_, err = hm.InstallChart(
				ctx,
				deps.METRICS_SERVER_RELEASE,
				deps.METRICS_SERVER_CHART,
				chartVersion,
				deps.METRICS_SERVER_NAMESPACE,
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

			l := logx.As()
			hm, err := helm.NewManager(helm.WithLogger(*l))
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			err = hm.UninstallChart(deps.METRICS_SERVER_RELEASE, deps.METRICS_SERVER_NAMESPACE)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			return automa.StepSuccessReport(stp.Id())
		})
}
