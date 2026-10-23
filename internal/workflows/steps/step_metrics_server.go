// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/cli/values"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/semver"
)

func DeployMetricsServer(valueOptions *values.Options) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("deploy-metrics-server").Steps(
		installMetricsServer(valueOptions, newHelmManager, func() (string, error) {
			return kube.ServerVersion(serverVersionTimeout)
		}),
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

// metrics-server 0.9 (chart 3.14) supports Kubernetes 1.34 and later. A cluster
// below that gets the newest chart that supports it, which the catalog keeps.
const (
	metricsServerMinKubeVersion     = "1.34.0"
	metricsServerLegacyChartVersion = "3.13.0"
	serverVersionTimeout            = 10 * time.Second
)

// serverVersionFunc returns the API server's version, such as "v1.36.5".
type serverVersionFunc func() (string, error)

func defaultMetricsServerValues() *values.Options {
	return &values.Options{
		Values: []string{
			"args={--kubelet-insecure-tls}",
			"apiService.insecureSkipTLSVerify=true",
		},
	}
}

// metricsServerChartFor picks the metrics-server chart for a cluster whose API
// server runs kubeVersion.
func metricsServerChartFor(kubeVersion string) (*helmChartSpec, error) {
	if strings.TrimSpace(kubeVersion) == "" {
		return nil, errorx.IllegalState.New("the API server reported an empty version")
	}
	v, err := semver.NewSemver(kubeVersion)
	if err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "cannot parse the API server version %q", kubeVersion)
	}
	floor, err := semver.NewSemver(metricsServerMinKubeVersion)
	if err != nil {
		return nil, errorx.AssertionFailed.Wrap(err, "invalid metrics-server Kubernetes floor")
	}
	if v.GreaterOrEqual(floor) {
		return resolveCatalogChart("metrics-server")
	}
	return resolveCatalogChartVersion("metrics-server", metricsServerLegacyChartVersion)
}

// installMetricsServerChart installs metrics-server unless its release already
// exists. It returns the chart it installed, or nil if it skipped. The chart
// depends on the cluster's Kubernetes version, so a version it cannot read stops
// the install rather than falling back to the newest chart.
func installMetricsServerChart(ctx context.Context, hm helm.Manager, defaults *helmChartSpec, valueOptions *values.Options, serverVersion serverVersionFunc) (*helmChartSpec, error) {
	isInstalled, err := hm.IsInstalled(defaults.Release, defaults.Namespace)
	if err != nil {
		return nil, err
	}
	if isInstalled {
		return nil, nil
	}

	kubeVersion, err := serverVersion()
	if err != nil {
		return nil, errx.Decorate(
			errorx.Decorate(err, "cannot read the Kubernetes version, so metrics-server was not installed"),
			reasons.K8sAPIFailed,
			"Check that the API server is up: kubectl get --raw /version",
			"Re-run the command once the API server answers")
	}
	spec, err := metricsServerChartFor(kubeVersion)
	if err != nil {
		return nil, errx.Decorate(
			errorx.Decorate(err, "cannot choose a metrics-server chart for this cluster, so metrics-server was not installed"),
			reasons.PreconditionNotMet,
			"Check the version the API server reports: kubectl version")
	}
	if spec.Version != defaults.Version {
		logx.As().Warn().Str("kubernetes", kubeVersion).Str("chart", spec.Version).
			Msgf("metrics-server chart %s needs Kubernetes %s or later; installing chart %s instead", defaults.Version, metricsServerMinKubeVersion, spec.Version)
	}

	if _, err := hm.AddRepo(spec.RepoAlias, spec.Repo, helm.RepoAddOptions{}); err != nil {
		return nil, err
	}

	localChart, err := hm.PullAndVerify(ctx, chartDownloadsDir(), spec.Chart, spec.Version, spec.Algorithm, spec.Checksum)
	if err != nil {
		return nil, err
	}

	if _, err := hm.InstallChart(
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
	); err != nil {
		return nil, err
	}

	return spec, nil
}

func installMetricsServer(valueOptions *values.Options, newManager func() (helm.Manager, error), serverVersion serverVersionFunc) *automa.StepBuilder {
	if valueOptions == nil {
		valueOptions = defaultMetricsServerValues()
	}

	spec := chartSpec("metrics-server")
	return automa.NewStepBuilder().WithId("enable-metrics-server").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			l := logx.As()
			hm, err := newManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			installed, err := installMetricsServerChart(ctx, hm, spec, valueOptions, serverVersion)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if installed == nil {
				meta[AlreadyInstalled] = "true"
				l.Info().Msg("Metrics server is already installed, skipping installation")
				return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
			}

			meta[InstalledByThisStep] = "true"
			meta["chartVersion"] = installed.Version
			stp.State().Local().Set(InstalledByThisStep, true)

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			if v, _ := stp.State().Local().Bool(InstalledByThisStep); v == false {
				return automa.StepSkippedReport(stp.Id())
			}

			hm, err := newManager()
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
