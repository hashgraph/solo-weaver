// SPDX-License-Identifier: Apache-2.0

package consensus

import "k8s.io/client-go/kubernetes"

// IsAuthError exposes the unexported isAuthError function for white-box tests.
var IsAuthError = isAuthError

// SetOnExecute injects a test hook that is called at the start of each handleExecute invocation.
func (um *UpgradeMonitor) SetOnExecute(fn func(operationID string)) {
	um.onExecute = fn
}

// NewNoPodRestartsWithClient constructs a NoPodRestarts criterion with a
// pre-built Kubernetes client, bypassing kubeconfig loading. For use in tests only.
func NewNoPodRestartsWithClient(client kubernetes.Interface, namespace, labelSelector string) *NoPodRestarts {
	return &NoPodRestarts{
		Namespace:        namespace,
		PodLabelSelector: labelSelector,
		client:           client,
	}
}

// WriteSoakState exposes the unexported writeSoakState helper for white-box tests.
func WriteSoakState(path string, req SoakStartRequest) error {
	return writeSoakState(path, req)
}
