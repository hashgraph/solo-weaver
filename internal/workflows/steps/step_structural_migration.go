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
	"helm.sh/helm/v3/pkg/release"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/hashgraph/solo-weaver/internal/alloy"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
)

// communityNodeExporterChart is the Helm chart name of the node-exporter the
// catalog manages; the Bitnami chart it replaces is named "node-exporter".
const communityNodeExporterChart = "prometheus-node-exporter"

var (
	externalSecretsGVR = schema.GroupVersionResource{Group: "external-secrets.io", Version: "v1", Resource: "externalsecrets"}
	serviceMonitorsGVR = schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "servicemonitors"}
)

// Seams so tests run without a cluster or Helm.
var (
	newStructuralHelm       = newHelmManager
	readNodeExporterHealth  = defaultReadNodeExporterHealth
	readESOHealth           = defaultReadESOHealth
	waitForPodsGone         = defaultWaitForPodsGone
	structuralHealthTimeout = 5 * time.Minute
	structuralPollInterval  = 5 * time.Second
)

func structuralHelm(stp automa.Step) (helm.Manager, *automa.Report) {
	hm, err := newStructuralHelm()
	if err != nil {
		return nil, automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.Internal)))
	}
	return hm, nil
}

func structuralClient(stp automa.Step, newClient kube.ClientProvider) (*kube.Client, *automa.Report) {
	k, err := newClient()
	if err != nil {
		return nil, automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.K8sAPIFailed,
			"Check that the admin kubeconfig works: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf get nodes")))
	}
	return k, nil
}

// waitHealthy polls problems until it returns none or the timeout passes.
func waitHealthy(ctx context.Context, stp automa.Step, what string, problems func() []string, hints ...string) *automa.Report {
	deadline := time.Now().Add(structuralHealthTimeout)
	var last []string
	for {
		if last = problems(); len(last) == 0 {
			return automa.SuccessReport(stp, automa.WithDetail(what+" is healthy"))
		}
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return automa.FailureReport(stp, automa.WithError(errx.Decorate(ctx.Err(), reasons.Internal)))
		case <-time.After(structuralPollInterval):
		}
	}
	slices.Sort(last)
	return automa.FailureReport(stp, automa.WithError(errx.Decorate(
		errorx.IllegalState.New("%s is not healthy after %s: %s", what, structuralHealthTimeout, strings.Join(last, "; ")),
		reasons.PreconditionNotMet, hints...)))
}

// ---- node-exporter: Bitnami chart -> prometheus-community chart ----

// nodeExporterSwap holds what the steps of the node-exporter swap share.
type nodeExporterSwap struct {
	spec       *helmChartSpec
	newClient  kube.ClientProvider
	done       bool
	legacy     bool // the Bitnami release is still there
	localChart string
}

// NodeExporterReplaceSteps replace the Bitnami node-exporter with the
// prometheus-community chart under the same release name: download the new
// chart, uninstall the old release and wait for its pods to go (both bind host
// port 9100), install the new chart, and wait until it is healthy. A swap that
// stopped after the uninstall finishes on a re-run; a done swap changes nothing.
func NodeExporterReplaceSteps(newClient kube.ClientProvider) []automa.Builder {
	s := &nodeExporterSwap{spec: chartSpec("node-exporter"), newClient: newClient}
	return []automa.Builder{s.prepare(), s.uninstallLegacy(), s.install(), s.waitHealthy()}
}

func (s *nodeExporterSwap) prepare() automa.Builder {
	return automa.NewStepBuilder().WithId("prepare-node-exporter-swap").
		WithPrepare(detail("downloading the prometheus-community node-exporter chart...")).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			hm, rpt := structuralHelm(stp)
			if rpt != nil {
				return rpt
			}
			rel, err := deployedRelease(hm, s.spec)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}
			switch {
			case rel == nil: // a swap that stopped after the uninstall
			case chartName(rel) == communityNodeExporterChart:
				s.done = true
				return automa.SkippedReport(stp, automa.WithDetail("node-exporter already runs the prometheus-community chart"))
			case chartName(rel) == "node-exporter":
				s.legacy = true
			default:
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(
					errorx.IllegalState.New("release %s in namespace %s runs chart %q, not a node-exporter chart this upgrade replaces",
						s.spec.Release, s.spec.Namespace, chartName(rel)),
					reasons.PreconditionNotMet,
					fmt.Sprintf("Inspect it: helm status %s -n %s", s.spec.Release, s.spec.Namespace))))
			}
			s.localChart, err = hm.PullAndVerify(ctx, chartDownloadsDir(), s.spec.Chart, s.spec.Version, s.spec.Algorithm, s.spec.Checksum)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.DownloadFailed,
					"Check that the host can reach ghcr.io, then re-run the upgrade; nothing was changed")))
			}
			return automa.SuccessReport(stp)
		})
}

func (s *nodeExporterSwap) uninstallLegacy() automa.Builder {
	return automa.NewStepBuilder().WithId("uninstall-legacy-node-exporter").
		WithPrepare(detail("removing the Bitnami node-exporter...")).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if s.done || !s.legacy {
				return automa.SkippedReport(stp, automa.WithDetail("no Bitnami node-exporter to remove"))
			}
			hm, rpt := structuralHelm(stp)
			if rpt != nil {
				return rpt
			}
			k, rpt := structuralClient(stp, s.newClient)
			if rpt != nil {
				return rpt
			}
			if err := hm.UninstallChart(s.spec.Release, s.spec.Namespace); err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.K8sAPIFailed,
					fmt.Sprintf("Check the release: helm status %s -n %s", s.spec.Release, s.spec.Namespace))))
			}
			if err := waitForPodsGone(ctx, k, s.spec.Namespace, alloy.NodeExporterAppName); err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.PreconditionNotMet,
					fmt.Sprintf("Check the pods: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf -n %s get pods", s.spec.Namespace),
					"Then run: sudo solo-provisioner alloy cluster install, which installs the prometheus-community node-exporter")))
			}
			return automa.SuccessReport(stp)
		})
}

func defaultWaitForPodsGone(ctx context.Context, k *kube.Client, namespace, prefix string) error {
	return k.WaitForResourcesDeletion(ctx, kube.KindPod, namespace, structuralHealthTimeout, kube.WaitOptions{NamePrefix: prefix})
}

func (s *nodeExporterSwap) install() automa.Builder {
	return automa.NewStepBuilder().WithId("install-node-exporter-chart").
		WithPrepare(detail("installing the prometheus-community node-exporter...")).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if s.done {
				return automa.SkippedReport(stp, automa.WithDetail("node-exporter already runs the prometheus-community chart"))
			}
			hm, rpt := structuralHelm(stp)
			if rpt != nil {
				return rpt
			}
			_, err := hm.InstallChart(ctx, s.spec.Release, s.localChart, "", s.spec.Namespace, helm.InstallChartOptions{
				ValueOpts:       alloy.NodeExporterHelmValues(),
				CreateNamespace: true,
				Atomic:          true,
				Wait:            true,
				Timeout:         helm.DefaultTimeout,
			})
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.K8sAPIFailed,
					fmt.Sprintf("Check the namespace: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf -n %s get pods,events", s.spec.Namespace),
					"Then run: sudo solo-provisioner alloy cluster install, which installs the prometheus-community node-exporter")))
			}
			return automa.SuccessReport(stp)
		})
}

// nodeExporterHealth is what the swap's health gate reads.
type nodeExporterHealth struct {
	Chart, Version  string
	Deployed        bool
	Desired, Ready  int64
	Updated         int64
	ServiceMonitors int // ServiceMonitors Alloy's selector picks up
}

func (h nodeExporterHealth) problems(version string) []string {
	var out []string
	if h.Chart != communityNodeExporterChart || h.Version != version || !h.Deployed {
		out = append(out, fmt.Sprintf("the release runs %s %s (deployed=%t), not %s %s", h.Chart, h.Version, h.Deployed, communityNodeExporterChart, version))
	}
	if h.Desired == 0 || h.Ready != h.Desired || h.Updated != h.Desired {
		out = append(out, fmt.Sprintf("DaemonSet: %d desired, %d updated, %d ready", h.Desired, h.Updated, h.Ready))
	}
	if h.ServiceMonitors != 1 {
		out = append(out, fmt.Sprintf("%d ServiceMonitors match Alloy's selector, want 1", h.ServiceMonitors))
	}
	return out
}

func defaultReadNodeExporterHealth(ctx context.Context, hm helm.Manager, k *kube.Client, spec *helmChartSpec) (nodeExporterHealth, error) {
	var h nodeExporterHealth
	rel, err := hm.GetRelease(spec.Release, spec.Namespace)
	if err != nil {
		return h, err
	}
	h.Chart, h.Deployed = chartName(rel), releaseStatus(rel) == release.StatusDeployed
	if rel != nil && rel.Chart != nil && rel.Chart.Metadata != nil {
		h.Version = rel.Chart.Metadata.Version
	}
	ds := func(field string) (int64, error) {
		v, _, err := k.GetResourceNestedInt64(ctx, "apps/v1", "DaemonSet", spec.Namespace, alloy.NodeExporterAppName, "status", field)
		return v, err
	}
	if h.Desired, err = ds("desiredNumberScheduled"); err != nil {
		return h, err
	}
	if h.Updated, err = ds("updatedNumberScheduled"); err != nil {
		return h, err
	}
	if h.Ready, err = ds("numberReady"); err != nil {
		return h, err
	}
	monitors, err := k.Dyn.Resource(serviceMonitorsGVR).Namespace(spec.Namespace).
		List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/name=" + alloy.NodeExporterAppName})
	if err != nil {
		return h, errorx.ExternalError.Wrap(err, "failed to list ServiceMonitors in %s", spec.Namespace)
	}
	h.ServiceMonitors = len(monitors.Items)
	return h, nil
}

func (s *nodeExporterSwap) waitHealthy() automa.Builder {
	return automa.NewStepBuilder().WithId("wait-node-exporter-healthy").
		WithPrepare(detail("waiting for node-exporter to be healthy...")).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			hm, rpt := structuralHelm(stp)
			if rpt != nil {
				return rpt
			}
			k, rpt := structuralClient(stp, s.newClient)
			if rpt != nil {
				return rpt
			}
			return waitHealthy(ctx, stp, "node-exporter "+s.spec.Version, func() []string {
				h, err := readNodeExporterHealth(ctx, hm, k, s.spec)
				if err != nil {
					return []string{err.Error()}
				}
				return h.problems(s.spec.Version)
			},
				fmt.Sprintf("Check the pods: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf -n %s get ds,pods,servicemonitors", s.spec.Namespace))
		})
}

// ---- External Secrets: chart 0.x -> 2.x ----

// esoMigration holds what the steps of the External Secrets migration share.
type esoMigration struct {
	to         string
	spec       *helmChartSpec // the target chart, with the release's name and namespace
	newClient  kube.ClientProvider
	done       bool
	localChart string
	wasReady   []string // ExternalSecrets Ready before the upgrade
}

// ExternalSecretsMigrateSteps move the External Secrets release from a 0.x
// chart to to: find the release wherever it was installed, download the new
// chart and note which ExternalSecrets are Ready, upgrade with our values, and
// wait until the controllers run to and those ExternalSecrets are Ready again.
// Both charts serve the same CRD API (v1), so no resource needs converting.
func ExternalSecretsMigrateSteps(to string, newClient kube.ClientProvider) []automa.Builder {
	m := &esoMigration{to: to, newClient: newClient}
	return []automa.Builder{m.prepare(), m.upgrade(), m.waitHealthy()}
}

func (m *esoMigration) prepare() automa.Builder {
	return automa.NewStepBuilder().WithId("prepare-external-secrets-migration").
		WithPrepare(detail("checking External Secrets before the upgrade...")).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			spec, err := resolveCatalogChartVersion(esoChartName, m.to)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.Internal)))
			}
			hm, rpt := structuralHelm(stp)
			if rpt != nil {
				return rpt
			}
			rel, err := theESORelease(hm, spec)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}
			spec.Release, spec.Namespace = rel.Name, rel.Namespace
			m.spec = spec
			if v := rel.Chart.Metadata.Version; v == m.to {
				m.done = true
				return automa.SkippedReport(stp, automa.WithDetail("External Secrets already runs "+m.to))
			}

			k, rpt := structuralClient(stp, m.newClient)
			if rpt != nil {
				return rpt
			}
			h, err := readESOHealth(ctx, hm, k, spec)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.K8sAPIFailed,
					"Check the API server: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf get --raw /readyz")))
			}
			m.wasReady = h.readyExternalSecrets()

			if _, err := hm.AddRepo(spec.RepoAlias, spec.Repo, helm.RepoAddOptions{}); err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.DownloadFailed,
					"Check that the host can reach "+spec.Repo+", then re-run the upgrade; nothing was changed")))
			}
			m.localChart, err = hm.PullAndVerify(ctx, chartDownloadsDir(), spec.Chart, spec.Version, spec.Algorithm, spec.Checksum)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.DownloadFailed,
					"Check that the host can reach "+spec.Repo+", then re-run the upgrade; nothing was changed")))
			}
			return automa.SuccessReport(stp, automa.WithDetail(fmt.Sprintf("%d ExternalSecrets are Ready", len(m.wasReady))))
		})
}

// theESORelease returns the only External Secrets release, wherever it was
// installed, refusing anything but one deployed release.
func theESORelease(hm helm.Manager, spec *helmChartSpec) (*release.Release, error) {
	releases, err := hm.ListAll()
	if err != nil {
		return nil, errx.Decorate(errorx.ExternalError.Wrap(err, "failed to list Helm releases"),
			reasons.K8sAPIFailed, "Check the API server: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf get --raw /readyz")
	}
	var found []*release.Release
	for _, rel := range releases {
		if rel != nil && rel.Chart != nil && rel.Chart.Metadata != nil && isESORelease(rel, spec) {
			found = append(found, rel)
		}
	}
	if len(found) != 1 {
		return nil, errx.Decorate(
			errorx.IllegalState.New("found %d External Secrets releases, want 1", len(found)),
			reasons.PreconditionNotMet, "List them: helm list -A")
	}
	rel := found[0]
	if releaseStatus(rel) != release.StatusDeployed {
		s := *spec
		s.Release, s.Namespace = rel.Name, rel.Namespace
		return nil, releaseRefused(&s, fmt.Sprintf("is in state %q, not deployed", releaseStatus(rel)))
	}
	return rel, nil
}

func (m *esoMigration) upgrade() automa.Builder {
	return automa.NewStepBuilder().WithId("upgrade-external-secrets").
		WithPrepare(detail("upgrading External Secrets to " + m.to + "...")).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if m.done {
				return automa.SkippedReport(stp, automa.WithDetail("External Secrets already runs "+m.to))
			}
			hm, rpt := structuralHelm(stp)
			if rpt != nil {
				return rpt
			}
			_, err := hm.UpgradeChart(ctx, m.spec.Release, m.localChart, "", m.spec.Namespace, helm.UpgradeChartOptions{
				ValueOpts: esoHelmValues(),
				Wait:      true,
				Timeout:   helm.DefaultTimeout,
			})
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.K8sAPIFailed,
					fmt.Sprintf("Check the release: helm history %s -n %s", m.spec.Release, m.spec.Namespace),
					fmt.Sprintf("To go back: helm rollback %s -n %s", m.spec.Release, m.spec.Namespace))))
			}
			return automa.SuccessReport(stp)
		})
}

// esoDeployment is one of the release's controller Deployments.
type esoDeployment struct {
	Desired, Available int64
	Image              string
}

// esoHealth is what the migration reads from the cluster.
type esoHealth struct {
	Chart, Version  string
	Deployed        bool
	Deployments     map[string]esoDeployment
	ExternalSecrets map[string]bool // namespace/name -> Ready
}

func (h esoHealth) readyExternalSecrets() []string {
	var out []string
	for name, ready := range h.ExternalSecrets {
		if ready {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// problems lists why h is not External Secrets on version with every
// ExternalSecret in wasReady Ready.
func (h esoHealth) problems(version string, wasReady []string) []string {
	var out []string
	if h.Chart != esoChartName || h.Version != version || !h.Deployed {
		out = append(out, fmt.Sprintf("the release runs %s %s (deployed=%t), not %s %s", h.Chart, h.Version, h.Deployed, esoChartName, version))
	}
	if len(h.Deployments) == 0 {
		out = append(out, "no controller Deployments")
	}
	for _, name := range sortedKeysOf(h.Deployments) {
		d := h.Deployments[name]
		if d.Desired == 0 || d.Available < d.Desired {
			out = append(out, fmt.Sprintf("%s: %d of %d available", name, d.Available, d.Desired))
		}
		if !strings.HasSuffix(d.Image, ":v"+version) {
			out = append(out, fmt.Sprintf("%s runs %s, not v%s", name, d.Image, version))
		}
	}
	for _, name := range wasReady {
		if !h.ExternalSecrets[name] {
			out = append(out, "ExternalSecret "+name+" was Ready before the upgrade and is not now")
		}
	}
	return out
}

func sortedKeysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func defaultReadESOHealth(ctx context.Context, hm helm.Manager, k *kube.Client, spec *helmChartSpec) (esoHealth, error) {
	h := esoHealth{Deployments: map[string]esoDeployment{}, ExternalSecrets: map[string]bool{}}
	rel, err := hm.GetRelease(spec.Release, spec.Namespace)
	if err != nil {
		return h, err
	}
	h.Chart, h.Deployed = chartName(rel), releaseStatus(rel) == release.StatusDeployed
	if rel != nil && rel.Chart != nil && rel.Chart.Metadata != nil {
		h.Version = rel.Chart.Metadata.Version
	}

	deps, err := k.Dyn.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}).
		Namespace(spec.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/instance=" + spec.Release})
	if err != nil {
		return h, errorx.ExternalError.Wrap(err, "failed to list Deployments in %s", spec.Namespace)
	}
	for _, d := range deps.Items {
		var e esoDeployment
		e.Desired, _, _ = unstructured.NestedInt64(d.Object, "spec", "replicas")
		e.Available, _, _ = unstructured.NestedInt64(d.Object, "status", "availableReplicas")
		containers, _, _ := unstructured.NestedSlice(d.Object, "spec", "template", "spec", "containers")
		if len(containers) > 0 {
			if c, ok := containers[0].(map[string]any); ok {
				e.Image, _ = c["image"].(string)
			}
		}
		h.Deployments[d.GetName()] = e
	}

	list, err := k.Dyn.Resource(externalSecretsGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return h, errorx.ExternalError.Wrap(err, "failed to list ExternalSecrets")
	}
	for _, es := range list.Items {
		h.ExternalSecrets[es.GetNamespace()+"/"+es.GetName()] = conditionTrue(es.Object, "Ready")
	}
	return h, nil
}

func (m *esoMigration) waitHealthy() automa.Builder {
	return automa.NewStepBuilder().WithId("wait-external-secrets-healthy").
		WithPrepare(detail("waiting for External Secrets to be healthy...")).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if m.spec == nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(
					errorx.IllegalState.New("the External Secrets release was not read"), reasons.Internal)))
			}
			hm, rpt := structuralHelm(stp)
			if rpt != nil {
				return rpt
			}
			k, rpt := structuralClient(stp, m.newClient)
			if rpt != nil {
				return rpt
			}
			return waitHealthy(ctx, stp, "External Secrets "+m.to, func() []string {
				h, err := readESOHealth(ctx, hm, k, m.spec)
				if err != nil {
					return []string{err.Error()}
				}
				return h.problems(m.to, m.wasReady)
			},
				fmt.Sprintf("Check the controllers: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf -n %s get deploy,pods", m.spec.Namespace),
				"Check the ExternalSecrets: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf get externalsecrets -A",
				fmt.Sprintf("To go back: helm rollback %s -n %s", m.spec.Release, m.spec.Namespace))
		})
}
