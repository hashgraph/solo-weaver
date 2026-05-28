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
)
