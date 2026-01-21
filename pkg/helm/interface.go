// SPDX-License-Identifier: Apache-2.0

package helm

import (
	"context"
	"time"

	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/repo"
)

type Status string

const (
	DefaultTimeout        = 15 * time.Minute
	StatusReady    Status = "ready"
	StatusDeleted  Status = "deleted"
)

// Manager defines the interface for managing Helm charts and releases
type Manager interface {
	// AddRepo adds a Helm chart repository with the given options and updates it.
	AddRepo(name, url string, o RepoAddOptions) (*repo.ChartRepository, error)

	// InstallChart installs a Helm chart with the given options
	InstallChart(ctx context.Context, releaseName, chartRef, chartVersion, namespace string, o InstallChartOptions) (*release.Release, error)

	// UninstallChart uninstalls a Helm release by name in the specified namespace
	UninstallChart(releaseName, namespace string) error

	// UpgradeChart upgrades a Helm chart with the given options
	UpgradeChart(ctx context.Context, releaseName, chartRef, chartVersion, namespace string, o UpgradeChartOptions) (*release.Release, error)

	// DeployChart installs or upgrades a Helm chart with the given options
	// This is equivalent to "helm upgrade --install"
	DeployChart(ctx context.Context, releaseName, chartRef, chartVersion, namespace string, o DeployChartOptions) (*release.Release, error)

	// List lists Helm releases in the specified namespace
	// It only lists releases in deployed state
	List(namespace string, allNamespaces bool) ([]*release.Release, error)

	// ListAll lists Helm releases in all namespaces
	// It only lists releases in deployed state
	ListAll() ([]*release.Release, error)

	// GetRelease retrieves a Helm release by name in the specified namespace
	GetRelease(releaseName, namespace string) (*release.Release, error)

	// IsInstalled checks if a Helm release is installed in the specified namespace
	// It considers only releases in deployed state as "installed"
	IsInstalled(releaseName, namespace string) (bool, error)

	// WaitFor waits until the Helm release reaches the desired status or the timeout is reached
	WaitFor(rel *release.Release, status Status, timeout time.Duration) error
}
