// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"
	"fmt"
	"time"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/cli/values"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// HelmOwnedServiceDeleteTimeout caps how long DeleteHelmOwnedServices waits for
// the API server to finalize deletion of the matched Services before failing
// the upgrade. Background propagation usually completes in <1s; the budget is
// generous enough to absorb a slow apiserver without masking a real stuck
// finalizer.
const HelmOwnedServiceDeleteTimeout = 30 * time.Second

// InstallChart installs the block node helm chart
func (m *Manager) InstallChart(ctx context.Context, valuesFile string) (bool, error) {
	isInstalled, err := m.helmManager.IsInstalled(m.blockNodeInputs.Release, m.blockNodeInputs.Namespace)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to check if block node is installed")
	}
	if isInstalled {
		m.logger.Info().Msg("Block Node is already installed, skipping installation")
		return false, nil
	}
	_, err = m.helmManager.InstallChart(
		ctx,
		m.blockNodeInputs.Release,
		m.blockNodeInputs.Chart,
		m.blockNodeInputs.ChartVersion,
		m.blockNodeInputs.Namespace,
		helm.InstallChartOptions{
			ValueOpts: &values.Options{
				ValueFiles: []string{valuesFile},
			},
			CreateNamespace: false,
			Atomic:          true,
			Wait:            true,
			Timeout:         helm.DefaultTimeout,
		},
	)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to install block node chart")
	}
	return true, nil
}

// UninstallChart uninstalls the block node helm chart
func (m *Manager) UninstallChart(ctx context.Context) error {
	return m.helmManager.UninstallChart(m.blockNodeInputs.Release, m.blockNodeInputs.Namespace)
}

// UpgradeChart upgrades the block node helm chart.
// When reuseValues is true and no custom values file is provided, the existing
// release values are reused unchanged (following Helm CLI convention).
func (m *Manager) UpgradeChart(ctx context.Context, valuesFile string, reuseValues bool) error {
	isInstalled, err := m.helmManager.IsInstalled(m.blockNodeInputs.Release, m.blockNodeInputs.Namespace)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to check if block node is installed")
	}
	if !isInstalled {
		return errorx.IllegalState.New("block node is not installed, cannot upgrade. Use 'install' command instead")
	}
	var valueFiles []string
	if valuesFile != "" {
		valueFiles = []string{valuesFile}
	} else if !reuseValues {
		return errorx.IllegalArgument.New("no values file provided and --no-reuse-values is set")
	}
	_, err = m.helmManager.UpgradeChart(
		ctx,
		m.blockNodeInputs.Release,
		m.blockNodeInputs.Chart,
		m.blockNodeInputs.ChartVersion,
		m.blockNodeInputs.Namespace,
		helm.UpgradeChartOptions{
			ValueOpts: &values.Options{
				ValueFiles: valueFiles,
			},
			ReuseValues: reuseValues,
			Atomic:      true,
			Wait:        true,
			Timeout:     helm.DefaultTimeout,
		},
	)
	if err != nil {
		m.logger.Error().
			Err(err).
			Str("chart", m.blockNodeInputs.Chart).
			Str("version", m.blockNodeInputs.ChartVersion).
			Str("namespace", m.blockNodeInputs.Namespace).
			Msg("Helm upgrade failed")
		return errorx.IllegalState.Wrap(err, "failed to upgrade block node chart")
	}
	return nil
}

// DeleteHelmOwnedServices deletes every Service in the block-node namespace that
// belongs to the current Helm release (labels `app.kubernetes.io/managed-by=Helm`
// and `app.kubernetes.io/instance=<release>`) and waits for the API server to
// confirm their removal.
//
// Run this immediately before `helm upgrade`. Helm's reconciliation phase will
// recreate the Services as part of the upgrade, which forces Cilium's eBPF
// service reconciler through a CREATE event rather than the `spec.type`
// transition UPDATE event it silently drops (see issue #619 / #644). Helm's
// 3-way merge handles the missing-from-cluster-but-in-last-release case
// transparently.
//
// A no-match is a successful no-op: a prior failed upgrade may have already
// removed the Services, and the headless / LB Services may genuinely not exist
// on certain chart variants. The caller (the saga step) is responsible for the
// post-upgrade reachability probe that turns any remaining failure mode into a
// loud workflow error.
func (m *Manager) DeleteHelmOwnedServices(ctx context.Context) error {
	selector := fmt.Sprintf(
		"app.kubernetes.io/managed-by=Helm,app.kubernetes.io/instance=%s",
		m.blockNodeInputs.Release,
	)
	opts := kube.WaitOptions{LabelSelector: selector}

	services, err := m.kubeClient.List(ctx, kube.KindService, m.blockNodeInputs.Namespace, opts)
	if err != nil {
		return errorx.IllegalState.Wrap(err,
			"failed to list helm-owned Services in namespace %s with selector %q",
			m.blockNodeInputs.Namespace, selector)
	}
	if len(services.Items) == 0 {
		m.logger.Info().
			Str("namespace", m.blockNodeInputs.Namespace).
			Str("selector", selector).
			Msg("No helm-owned Services to delete before upgrade")
		return nil
	}

	gvr, err := kube.ToGroupVersionResource(kube.KindService)
	if err != nil {
		return err
	}
	policy := metav1.DeletePropagationBackground

	names := make([]string, 0, len(services.Items))
	for i := range services.Items {
		names = append(names, services.Items[i].GetName())
	}
	m.logger.Info().
		Strs("services", names).
		Str("namespace", m.blockNodeInputs.Namespace).
		Msg("Deleting helm-owned Services before upgrade so Cilium reconciles a fresh CREATE")

	for _, name := range names {
		err := m.kubeClient.Dyn.Resource(gvr).Namespace(m.blockNodeInputs.Namespace).
			Delete(ctx, name, metav1.DeleteOptions{PropagationPolicy: &policy})
		if err != nil && !kerrors.IsNotFound(err) {
			return errorx.IllegalState.Wrap(err,
				"failed to delete Service %s/%s",
				m.blockNodeInputs.Namespace, name)
		}
		m.logger.Debug().
			Str("service", name).
			Str("namespace", m.blockNodeInputs.Namespace).
			Msg("Service delete submitted")
	}

	if err := m.kubeClient.WaitForResourcesDeletion(ctx, kube.KindService, m.blockNodeInputs.Namespace, HelmOwnedServiceDeleteTimeout, opts); err != nil {
		return errorx.IllegalState.Wrap(err,
			"helm-owned Services in namespace %s were not fully deleted within %s",
			m.blockNodeInputs.Namespace, HelmOwnedServiceDeleteTimeout)
	}
	return nil
}

// DeleteStatefulSetForUpgrade deletes the block node StatefulSet using orphan cascading
// and waits for it to be fully removed from the API server. This is required before any
// Helm upgrade that changes volumeClaimTemplates, since Kubernetes forbids in-place updates.
// Returns nil if the StatefulSet does not exist.
func (m *Manager) DeleteStatefulSetForUpgrade(ctx context.Context) error {
	stsName := m.blockNodeInputs.Release + ResourceNameSuffix
	m.logger.Info().
		Str("statefulset", stsName).
		Msg("Deleting StatefulSet (orphan cascade) to allow volumeClaimTemplates update")
	if err := m.kubeClient.DeleteStatefulSet(ctx, m.blockNodeInputs.Namespace, stsName); err != nil {
		m.logger.Warn().Err(err).Str("statefulset", stsName).Msg("Failed to delete StatefulSet before upgrade")
		return errorx.ExternalError.Wrap(err, "failed to delete StatefulSet before upgrade")
	}
	m.logger.Info().Str("statefulset", stsName).Msg("Waiting for StatefulSet deletion to complete")
	waitTimeout := 60 * time.Second
	if err := m.kubeClient.WaitForResource(ctx, kube.KindStatefulSet, m.blockNodeInputs.Namespace, stsName, kube.IsDeleted, waitTimeout); err != nil {
		m.logger.Warn().Err(err).Str("statefulset", stsName).Msg("Timeout waiting for StatefulSet deletion, proceeding with upgrade attempt")
	}
	return nil
}

// ScaleStatefulSet scales the block node statefulset to the specified number of replicas
func (m *Manager) ScaleStatefulSet(ctx context.Context, replicas int32) error {
	resourceName := m.blockNodeInputs.Release + ResourceNameSuffix
	m.logger.Info().
		Str("statefulset", resourceName).
		Int32("replicas", replicas).
		Msg("Scaling block node statefulset")
	if err := m.kubeClient.ScaleStatefulSet(ctx, m.blockNodeInputs.Namespace, resourceName, replicas); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to scale statefulset: %s", resourceName)
	}
	return nil
}

// WaitForPodsTerminated waits until all pods matching the block node label selector are terminated
func (m *Manager) WaitForPodsTerminated(ctx context.Context) error {
	m.logger.Info().Msg("Waiting for Block Node pods to terminate...")
	timeout := time.Duration(PodReadyTimeoutSeconds) * time.Second
	opts := kube.WaitOptions{LabelSelector: PodLabelSelector}
	if err := m.kubeClient.WaitForResourcesDeletion(ctx, kube.KindPod, m.blockNodeInputs.Namespace, timeout, opts); err != nil {
		return errorx.IllegalState.Wrap(err, "pods did not terminate in time")
	}
	return nil
}

// WaitForPodReady waits for the block node pod to be ready
func (m *Manager) WaitForPodReady(ctx context.Context) error {
	m.logger.Info().Msg("Waiting for Block Node pod to be ready...")
	timeout := time.Duration(PodReadyTimeoutSeconds) * time.Second
	opts := kube.WaitOptions{LabelSelector: PodLabelSelector}
	if err := m.kubeClient.WaitForResources(ctx, kube.KindPod, m.blockNodeInputs.Namespace, kube.IsPodReady, timeout, opts); err != nil {
		return errorx.IllegalState.Wrap(err, "pod did not become ready in time")
	}
	return nil
}

// GetTargetVersion returns the configured target version for block node.
func (m *Manager) GetTargetVersion() string {
	return m.blockNodeInputs.ChartVersion
}

// GetInstalledVersion returns the currently installed Block Node chart version.
// Returns empty string if not installed.
func (m *Manager) GetInstalledVersion() (string, error) {
	rel, err := m.helmManager.GetRelease(m.blockNodeInputs.Release, m.blockNodeInputs.Namespace)
	if err != nil {
		if errorx.IsOfType(err, helm.ErrNotFound) {
			return "", nil
		}
		return "", errorx.IllegalState.Wrap(err, "failed to get current release")
	}
	if rel.Chart != nil && rel.Chart.Metadata != nil {
		return rel.Chart.Metadata.Version, nil
	}
	return "", nil
}

// GetReleaseValues returns the user-supplied values from the currently installed release.
// Returns nil if not installed or if no user values were supplied.
func (m *Manager) GetReleaseValues() (map[string]interface{}, error) {
	rel, err := m.helmManager.GetRelease(m.blockNodeInputs.Release, m.blockNodeInputs.Namespace)
	if err != nil {
		if errorx.IsOfType(err, helm.ErrNotFound) {
			return nil, nil
		}
		return nil, errorx.IllegalState.Wrap(err, "failed to get current release")
	}
	// rel.Config contains the user-supplied values (not the computed/merged values)
	return rel.Config, nil
}
