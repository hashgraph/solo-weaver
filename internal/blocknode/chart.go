// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"
	"time"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/cli/values"
)

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

// AnnotateService annotates the block node service with MetalLB address pool
func (m *Manager) AnnotateService(ctx context.Context) error {
	resourceName := m.blockNodeInputs.Release + ResourceNameSuffix
	annotations := map[string]string{
		"metallb.io/address-pool": "public-address-pool",
	}
	if err := m.kubeClient.AnnotateResource(ctx, kube.KindService, m.blockNodeInputs.Namespace, resourceName, annotations); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to annotate service: %s", resourceName)
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
