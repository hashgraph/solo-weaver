// SPDX-License-Identifier: Apache-2.0

package consensus

import "github.com/joomcode/errorx"

var (
	ErrNamespace = errorx.NewNamespace("daemon.consensus")

	// ErrK8sClient is returned when the Kubernetes dynamic client cannot be built
	// (e.g. kubeconfig missing or malformed).
	ErrK8sClient = ErrNamespace.NewType("k8s_client")

	// ErrWatchFailed is returned when the Kubernetes watch API call fails or
	// returns an error event (e.g. 401/403, server disconnect).
	ErrWatchFailed = ErrNamespace.NewType("watch_failed")

	// ErrSoakWatcher is returned when the soak watcher encounters an error
	// (e.g. state file I/O, decommission failure).
	ErrSoakWatcher = ErrNamespace.NewType("soak_watcher")

	// ErrK8sAPI is returned when a Kubernetes API call (Get/Patch/List) fails during
	// the execute phase. Carries the errorx.Temporary() trait: the execute driver
	// classifies it as a transient failure and retries via watch re-delivery rather
	// than writing DaemonResult=False (HIP-1496 Provisioner Failure Semantics). An
	// execute-phase error WITHOUT this trait is treated as fatal.
	ErrK8sAPI = ErrNamespace.NewType("k8s_api", errorx.Temporary())

	// ErrUpgradePath is returned when reading the staged upgrade package fails
	// (disk/IO). Temporary — the package may still be settling; retried via
	// re-delivery until it is readable or the handoff deadline elapses.
	ErrUpgradePath = ErrNamespace.NewType("upgrade_path", errorx.Temporary())

	// ErrConfigFileInvalid is returned when a config file in the upgrade package is
	// fundamentally unusable — a symlink, oversized, or an unrecognised filename.
	// FATAL (no Temporary trait): re-reading the same bytes cannot fix it, so the
	// daemon reports DaemonResult=False rather than retrying.
	ErrConfigFileInvalid = ErrNamespace.NewType("config_file_invalid")

	// ErrConfigCRInvalid is returned when the operator reconciles a config CR to
	// Valid=False (it rejected the content). FATAL — DaemonResult=False, no retry.
	ErrConfigCRInvalid = ErrNamespace.NewType("config_cr_invalid")

	// ErrInfraVersionsPlace is returned when writing infrastructure-versions.yaml to
	// the trusted host location fails (mkdir/write/rename). Temporary — a filesystem
	// hiccup is retried via re-delivery until the handoff deadline.
	ErrInfraVersionsPlace = ErrNamespace.NewType("infra_versions_place", errorx.Temporary())
)
