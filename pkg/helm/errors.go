// SPDX-License-Identifier: Apache-2.0

package helm

import "github.com/joomcode/errorx"

var (
	ErrNamespace = errorx.NewNamespace("helm")

	ErrNotFound               = ErrNamespace.NewType("not_found", errorx.NotFound()) // Release or resource not found
	ErrRepoInvalid            = ErrNamespace.NewType("repo_invalid")                 // Repo invalid or unreachable
	ErrChartDependencyMissing = ErrNamespace.NewType("chart_dependency_missing")     // Chart dependencies missing
	ErrChartLoadFailed        = ErrNamespace.NewType("chart_load_failed")            // Failed to load chart from path or repo
	ErrInstallFailed          = ErrNamespace.NewType("install_failed")               // Helm install failed
	ErrUpgradeFailed          = ErrNamespace.NewType("upgrade_failed")               // Helm upgrade failed
	ErrUninstallFailed        = ErrNamespace.NewType("uninstall_failed")             // Helm uninstall failed
	ErrWaitTimeout            = ErrNamespace.NewType("wait_timeout", errorx.Timeout())
)
