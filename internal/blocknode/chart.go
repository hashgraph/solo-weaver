// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"
	"time"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/joomcode/errorx"
)

// Namespace returns the block-node Kubernetes namespace these operations target.
func (m *Manager) Namespace() string {
	return m.blockNodeInputs.Namespace
}

// UninstallChart uninstalls the block node helm chart
func (m *Manager) UninstallChart(ctx context.Context) error {
	return m.helmManager.UninstallChart(m.blockNodeInputs.Release, m.blockNodeInputs.Namespace)
}

// IsInstalled reports whether a Helm release for this block node currently
// exists. It lets the uninstall step treat a missing legacy release as a no-op
// (idempotent uninstall) rather than erroring on a "release: not found".
func (m *Manager) IsInstalled() (bool, error) {
	return m.helmManager.IsInstalled(m.blockNodeInputs.Release, m.blockNodeInputs.Namespace)
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
