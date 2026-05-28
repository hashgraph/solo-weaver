// SPDX-License-Identifier: Apache-2.0

package consensus

// IsAuthError exposes the unexported isAuthError function for white-box tests.
var IsAuthError = isAuthError

// SetOnExecute injects a test hook that is called at the start of each handleExecute invocation.
func (um *UpgradeMonitor) SetOnExecute(fn func(operationID string)) {
	um.onExecute = fn
}
