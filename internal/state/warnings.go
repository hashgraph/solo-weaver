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

// AddWarning records a warning for the operator from outside this package. It is
// for startup migrations: their log lines reach only the log file in the
// interactive mode, while these warnings reach the command's summary, or stderr
// when it has none.
func AddWarning(w string) { addWarning(w) }

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
