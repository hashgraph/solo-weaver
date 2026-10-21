// SPDX-License-Identifier: Apache-2.0

package state

import (
	"slices"
	"sync"
)

// warnings holds problems the state package reports to the operator without
// failing the command, such as a failed sync of the weaver home directory.
var (
	warningsMu sync.Mutex
	warnings   []string
)

// addWarning records a warning for the operator, once: each state write that
// hits the same problem would otherwise add another copy.
func addWarning(w string) {
	warningsMu.Lock()
	defer warningsMu.Unlock()
	if slices.Contains(warnings, w) {
		return
	}
	warnings = append(warnings, w)
}

// TakeWarnings returns the warnings recorded since the last call and clears
// them. The command shows them in its summary.
func TakeWarnings() []string {
	warningsMu.Lock()
	defer warningsMu.Unlock()
	w := warnings
	warnings = nil
	return w
}
