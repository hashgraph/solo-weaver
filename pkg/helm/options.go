// SPDX-License-Identifier: Apache-2.0

package helm

import (
	"time"

	"helm.sh/helm/v3/pkg/cli/values"
)

// RepoAddOptions holds options for adding a Helm repo
type RepoAddOptions struct {
	RepoFile  string
	RepoCache string
}

// InstallChartOptions for installing Helm charts
type InstallChartOptions struct {
	ValueOpts       *values.Options
	Atomic          bool
	Wait            bool
	Timeout         time.Duration
	CreateNamespace bool
}

// UpgradeChartOptions for upgrading Helm charts
type UpgradeChartOptions struct {
	ValueOpts   *values.Options
	Atomic      bool
	Wait        bool
	Timeout     time.Duration
	ReuseValues bool
}

// DeployChartOptions for idempotent install/upgrade
type DeployChartOptions struct {
	ValueOpts       *values.Options
	Atomic          bool
	Wait            bool
	Timeout         time.Duration
	CreateNamespace bool
	ReuseValues     bool
}
