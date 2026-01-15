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
	"github.com/joomcode/errorx"
)

const (
	PrometheusOperatorCRDsNamespace = "grafana-alloy"
	PrometheusOperatorCRDsRelease   = "prometheus-operator-crds"
	PrometheusOperatorCRDsChart     = "oci://ghcr.io/prometheus-community/charts/prometheus-operator-crds"
	PrometheusOperatorCRDsVersion   = "24.0.1"
	SetupPrometheusCRDsStepId       = "setup-prometheus-crds"
	InstallPrometheusCRDsStepId     = "install-prometheus-crds"
	IsPrometheusCRDsReadyStepId     = "is-prometheus-crds-ready"
)

// SetupPrometheusOperatorCRDs returns a workflow builder that sets up Prometheus Operator CRDs.
// These CRDs are required for ServiceMonitor and PodMonitor support in Alloy.
func SetupPrometheusOperatorCRDs() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId(SetupPrometheusCRDsStepId).Steps(
		installPrometheusOperatorCRDs(),
		isPrometheusOperatorCRDsReady(),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up Prometheus Operator CRDs")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup Prometheus Operator CRDs")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Prometheus Operator CRDs setup successfully")
		})
}

func installPrometheusOperatorCRDs() automa.Builder {
	return automa.NewStepBuilder().WithId(InstallPrometheusCRDsStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			l := logx.As()
			hm, err := helm.NewManager(helm.WithLogger(*l))
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			isInstalled, err := hm.IsInstalled(PrometheusOperatorCRDsRelease, PrometheusOperatorCRDsNamespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if isInstalled {
				meta[AlreadyInstalled] = "true"
				l.Info().Msg("Prometheus Operator CRDs are already installed, skipping installation")
				return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
			}

			_, err = hm.InstallChart(
				ctx,
				PrometheusOperatorCRDsRelease,
				PrometheusOperatorCRDsChart,
				PrometheusOperatorCRDsVersion,
				PrometheusOperatorCRDsNamespace,
				helm.InstallChartOptions{
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

			err = hm.UninstallChart(PrometheusOperatorCRDsRelease, PrometheusOperatorCRDsNamespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Installing Prometheus Operator CRDs")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to install Prometheus Operator CRDs")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Prometheus Operator CRDs installed successfully")
		})
}

func isPrometheusOperatorCRDsReady() automa.Builder {
	return automa.NewStepBuilder().WithId(IsPrometheusCRDsReadyStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			k, err := kube.NewClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			// Verify ServiceMonitor CRD exists
			exists, err := k.CRDExists(ctx, "servicemonitors.monitoring.coreos.com")
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if !exists {
				err := errorx.IllegalState.New("ServiceMonitor CRD not found - Prometheus Operator CRDs may not be installed correctly")
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// Wait a bit for CRDs to be fully registered
			time.Sleep(5 * time.Second)

			meta[IsReady] = "true"
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Verifying Prometheus Operator CRDs readiness")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Prometheus Operator CRDs are not ready")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Prometheus Operator CRDs are ready")
		})
}
