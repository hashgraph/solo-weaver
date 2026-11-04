// SPDX-License-Identifier: Apache-2.0

// Package drift finds fields of a managed component whose live value differs
// from its persisted baseline, comparing them before a refresh folds the live
// values into state.
//
// Each component supplies a Producer that owns its comparison rules. This
// package only collects and formats what the producers return.
package drift

import (
	"fmt"

	"github.com/hashgraph/solo-weaver/internal/state"
)

// Change is one field whose persisted value differs from the live system.
type Change struct {
	// Component names the managed component, such as "teleport".
	Component string
	// Field is the field's key path under the component, such as "nodeAgent.installed".
	Field     string
	Persisted string
	Live      string
	// Removed is set when the change is the removal of what Field belongs to,
	// such as a Teleport agent uninstalled out of band.
	Removed bool
	// Unobserved is set when this run could not read the live value at all,
	// such as a Teleport cluster agent behind an API server that did not
	// answer. Live is then empty.
	Unobserved bool
}

// String renders the change as one operator-facing warning line.
func (c Change) String() string {
	if c.Unobserved {
		return fmt.Sprintf("%s %s could not be read from the live system: recorded %q",
			c.Component, c.Field, c.Persisted)
	}
	return fmt.Sprintf("%s %s differs from persisted state: recorded %q, live %q",
		c.Component, c.Field, c.Persisted, c.Live)
}

// Producer compares one component's persisted baseline with the live state.
//
// It must return only differences an operator should act on. A field the live
// system can never report is left out. A part it could not read on this run is
// reported once, as an Unobserved change on the field that says whether the
// part exists, so a destructive action never takes "not read" for "unchanged".
// A change that reports something gone from the live system must set Removed:
// no action can destroy a removal, so the drift guard never stops a run for one.
type Producer func(baseline, live state.State) []Change

// Detect runs every producer against the same baseline and live state, keeping
// the producers' order.
func Detect(baseline, live state.State, producers ...Producer) []Change {
	var changes []Change
	for _, p := range producers {
		changes = append(changes, p(baseline, live)...)
	}
	return changes
}
