// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/release"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/hashgraph/solo-weaver/internal/alloy"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// ChartBump moves one catalog chart's release from one version to another.
type ChartBump struct {
	Chart, From, To string
}

// chartRecipe is how a charts hop upgrades one catalog chart.
type chartRecipe struct {
	// values are the Helm values the target version gets. Never nil: a nil
	// value set makes the Helm manager reuse the release's values.
	values func(to string) (*values.Options, error)
	// reuse keeps the values the release was installed with under values.
	reuse bool
	// find returns the release; nil means the catalog release name and namespace.
	find func(hm helm.Manager, spec *helmChartSpec) (*release.Release, error)
	// extra lists what is wrong beyond Helm's --wait; nil means nothing.
	extra func(ctx context.Context, k *kube.Client, spec *helmChartSpec) ([]string, error)
}

func fixedValues(v *values.Options) func(string) (*values.Options, error) {
	return func(string) (*values.Options, error) { return v, nil }
}

// chartRecipes are the charts a charts hop can upgrade. Alloy is built from
// `alloy cluster install` inputs that are not saved anywhere, so it keeps the
// release's values and only gets what its target version needs.
var chartRecipes = map[string]chartRecipe{
	"metallb":        {values: fixedValues(&values.Options{Values: metalLBHelmValues()})},
	"metrics-server": {values: fixedValues(defaultMetricsServerValues()), extra: metricsAPIProblems},
	"prometheus-operator-crds": {
		values: fixedValues(&values.Options{}),
		extra:  crdsEstablishedProblems,
	},
	"external-secrets": {values: fixedValues(esoHelmValues()), find: theESORelease},
	"node-exporter":    {values: fixedValues(alloy.NodeExporterHelmValues())},
	"alloy": {
		values: func(to string) (*values.Options, error) {
			json, err := alloy.ChartJSONValues(to)
			if err != nil {
				return nil, err
			}
			return &values.Options{JSONValues: json}, nil
		},
		reuse: true,
	},
}

// crdCharts move before the charts whose resources they define.
var crdCharts = []string{"prometheus-operator-crds"}

// ChartHasRecipe reports whether a charts hop can upgrade the named chart.
func ChartHasRecipe(chart string) bool {
	_, ok := chartRecipes[chart]
	return ok
}

// orderBumps puts CRD charts first and keeps the plan's order otherwise.
func orderBumps(bumps []ChartBump) []ChartBump {
	out := slices.Clone(bumps)
	slices.SortStableFunc(out, func(a, b ChartBump) int {
		ai, bi := slices.Contains(crdCharts, a.Chart), slices.Contains(crdCharts, b.Chart)
		switch {
		case ai && !bi:
			return -1
		case bi && !ai:
			return 1
		}
		return 0
	})
	return out
}

// Seams so tests run without a cluster or Helm.
var (
	newChartHopHelm      = newHelmManager
	chartHopServerVer    = func() (string, error) { return kube.ServerVersion(10 * time.Second) }
	chartHopPollInterval = 5 * time.Second
	chartHopTimeout      = 5 * time.Minute
)

// ChartHopSteps upgrade each chart of a charts hop to its target, CRD charts
// first: check the hop suits the running Kubernetes, then for each chart
// download and verify the target, `helm upgrade --wait`, and wait until the
// release is deployed at the target and the chart's own checks pass. A chart
// already at its target is skipped, so a re-run finishes a hop that stopped
// part way. The first failure stops the hop; charts already moved stay.
func ChartHopSteps(bumps []ChartBump, newClient kube.ClientProvider) []automa.Builder {
	ordered := orderBumps(bumps)
	out := []automa.Builder{checkChartHop(ordered)}
	for _, b := range ordered {
		out = append(out, upgradeChart(b, newClient))
	}
	return out
}

func checkChartHop(bumps []ChartBump) automa.Builder {
	return automa.NewStepBuilder().WithId("check-chart-hop").
		WithPrepare(detail("checking the charts hop...")).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			for _, b := range bumps {
				if !ChartHasRecipe(b.Chart) {
					return automa.FailureReport(stp, automa.WithError(errx.Decorate(
						errorx.IllegalArgument.New("this binary cannot upgrade chart %s", b.Chart), reasons.Internal)))
				}
				if b.Chart != "metrics-server" {
					continue
				}
				// metrics-server's chart depends on the Kubernetes minor.
				kv, err := chartHopServerVer()
				var spec *helmChartSpec
				if err == nil {
					spec, err = metricsServerChartFor(kv)
				}
				if err == nil && spec.Version != b.To {
					err = errorx.IllegalState.New("metrics-server %s is not the chart for Kubernetes %s (that is %s)", b.To, kv, spec.Version)
				}
				if err != nil {
					return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.PreconditionNotMet,
						"Run 'solo-provisioner kube cluster upgrade --plan --experimental' to see the next hop for this cluster")))
				}
			}
			return automa.SuccessReport(stp)
		})
}

func upgradeChart(b ChartBump, newClient kube.ClientProvider) automa.Builder {
	recipe := chartRecipes[b.Chart]
	return automa.NewStepBuilder().WithId("upgrade-chart-" + b.Chart).
		WithPrepare(detail(fmt.Sprintf("upgrading %s to %s...", b.Chart, b.To))).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			spec, err := resolveCatalogChartVersion(b.Chart, b.To)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.Internal)))
			}
			hm, err := newChartHopHelm()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.Internal)))
			}
			find := recipe.find
			if find == nil {
				find = deployedRelease
			}
			rel, err := find(hm, spec)
			if err == nil && rel == nil {
				err = errorx.IllegalState.New("chart %s has no release to upgrade", b.Chart)
			}
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.PreconditionNotMet,
					"Run 'solo-provisioner kube cluster upgrade --plan --experimental' to see the next hop for this cluster")))
			}
			spec.Release, spec.Namespace = rel.Name, rel.Namespace
			live := rel.Chart.Metadata.Version

			k, err := newClient()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.K8sAPIFailed,
					"Check that the admin kubeconfig works: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf get nodes")))
			}

			if live != b.To {
				if live != b.From {
					return automa.FailureReport(stp, automa.WithError(errx.Decorate(
						errorx.IllegalState.New("%s runs %s, not the hop's start %s", b.Chart, live, b.From),
						reasons.PreconditionNotMet,
						"Run 'solo-provisioner kube cluster upgrade --plan --experimental' to see the next hop for this cluster")))
				}
				if rpt := runChartUpgrade(ctx, stp, hm, spec, recipe, b.To); rpt != nil {
					return rpt
				}
			}

			deadline := time.Now().Add(chartHopTimeout)
			var last []string
			for {
				last = chartProblems(ctx, hm, k, spec, b.To, recipe.extra)
				if len(last) == 0 {
					if live == b.To {
						return automa.SkippedReport(stp, automa.WithDetail(fmt.Sprintf("%s already runs %s", b.Chart, b.To)))
					}
					return automa.SuccessReport(stp, automa.WithDetail(fmt.Sprintf("%s runs %s", b.Chart, b.To)))
				}
				if time.Now().After(deadline) {
					break
				}
				select {
				case <-ctx.Done():
					return automa.FailureReport(stp, automa.WithError(errx.Decorate(ctx.Err(), reasons.Internal)))
				case <-time.After(chartHopPollInterval):
				}
			}
			return automa.FailureReport(stp, automa.WithError(errx.Decorate(
				errorx.IllegalState.New("%s %s is not healthy after %s: %s", b.Chart, b.To, chartHopTimeout, strings.Join(last, "; ")),
				reasons.PreconditionNotMet,
				fmt.Sprintf("Check the release: helm status %s -n %s", spec.Release, spec.Namespace),
				fmt.Sprintf("To go back: helm rollback %s -n %s", spec.Release, spec.Namespace))))
		})
}

func runChartUpgrade(ctx context.Context, stp automa.Step, hm helm.Manager, spec *helmChartSpec, recipe chartRecipe, to string) *automa.Report {
	vals, err := recipe.values(to)
	if err != nil {
		return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.Internal)))
	}
	if spec.Type == software.ChartTypeClassic {
		if _, err := hm.AddRepo(spec.RepoAlias, spec.Repo, helm.RepoAddOptions{}); err != nil {
			return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.DownloadFailed,
				"Check that the host can reach "+spec.Repo+", then re-run the upgrade")))
		}
	}
	local, err := hm.PullAndVerify(ctx, chartDownloadsDir(), spec.Chart, spec.Version, spec.Algorithm, spec.Checksum)
	if err != nil {
		return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.DownloadFailed,
			fmt.Sprintf("Check that the host can download %s %s, then re-run the upgrade", spec.Chart, spec.Version))))
	}
	_, err = hm.UpgradeChart(ctx, spec.Release, local, "", spec.Namespace, helm.UpgradeChartOptions{
		ValueOpts:   vals,
		ReuseValues: recipe.reuse,
		Wait:        true,
		Timeout:     helm.DefaultTimeout,
	})
	if err != nil {
		return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.K8sAPIFailed,
			fmt.Sprintf("Check the release: helm history %s -n %s", spec.Release, spec.Namespace),
			fmt.Sprintf("To go back: helm rollback %s -n %s", spec.Release, spec.Namespace))))
	}
	return nil
}

// chartProblems lists why the release is not deployed and healthy at to.
func chartProblems(ctx context.Context, hm helm.Manager, k *kube.Client, spec *helmChartSpec, to string,
	extra func(context.Context, *kube.Client, *helmChartSpec) ([]string, error)) []string {
	rel, err := hm.GetRelease(spec.Release, spec.Namespace)
	if err != nil {
		return []string{err.Error()}
	}
	var out []string
	version := ""
	if rel != nil && rel.Chart != nil && rel.Chart.Metadata != nil {
		version = rel.Chart.Metadata.Version
	}
	if version != to || releaseStatus(rel) != release.StatusDeployed {
		out = append(out, fmt.Sprintf("the release is %s %s, not deployed at %s", version, releaseStatus(rel), to))
	}
	if extra != nil {
		more, err := extra(ctx, k, spec)
		if err != nil {
			more = []string{err.Error()}
		}
		out = append(out, more...)
	}
	return out
}

var (
	apiServicesGVR = schema.GroupVersionResource{Group: "apiregistration.k8s.io", Version: "v1", Resource: "apiservices"}
	crdsGVR        = schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}
)

const metricsAPIService = "v1beta1.metrics.k8s.io"

func metricsAPIProblems(ctx context.Context, k *kube.Client, _ *helmChartSpec) ([]string, error) {
	svc, err := k.Dyn.Resource(apiServicesGVR).Get(ctx, metricsAPIService, metav1.GetOptions{})
	if err != nil {
		return nil, errorx.ExternalError.Wrap(err, "failed to read APIService %s", metricsAPIService)
	}
	if !conditionTrue(svc.Object, "Available") {
		return []string{"APIService " + metricsAPIService + " is not Available"}, nil
	}
	return nil, nil
}

func crdsEstablishedProblems(ctx context.Context, k *kube.Client, spec *helmChartSpec) ([]string, error) {
	list, err := k.Dyn.Resource(crdsGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, errorx.ExternalError.Wrap(err, "failed to list CRDs")
	}
	var out []string
	owned := 0
	for _, crd := range list.Items {
		a := crd.GetAnnotations()
		if a["meta.helm.sh/release-name"] != spec.Release || a["meta.helm.sh/release-namespace"] != spec.Namespace {
			continue
		}
		owned++
		if !conditionTrue(crd.Object, "Established") {
			out = append(out, "CRD "+crd.GetName()+" is not Established")
		}
	}
	if owned == 0 {
		out = append(out, fmt.Sprintf("no CRDs belong to release %s/%s", spec.Namespace, spec.Release))
	}
	slices.Sort(out)
	return out, nil
}
