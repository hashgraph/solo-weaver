// SPDX-License-Identifier: Apache-2.0

// Package drift finds fields of a managed component that were changed outside
// solo-provisioner, by comparing the persisted state.yaml baseline with what the
// live system reports, before a refresh folds the live values into state.
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
}

// String renders the change as one operator-facing warning line.
func (c Change) String() string {
	return fmt.Sprintf("%s %s changed outside solo-provisioner: state.yaml has %q, live is %q",
		c.Component, c.Field, c.Persisted, c.Live)
}

// Producer compares one component's persisted baseline with the live state.
//
// It must return only differences an operator should act on. A field the live
// system cannot report, or could not report on this run, is left out rather
// than reported as changed.
type Producer func(baseline, live state.State) []Change

// DefaultProducers returns the producer of every component wired for detection.
func DefaultProducers() []Producer {
	return []Producer{Teleport}
}

// Detect runs every producer against the same baseline and live state, keeping
// the producers' order.
func Detect(baseline, live state.State, producers ...Producer) []Change {
	var changes []Change
	for _, p := range producers {
		changes = append(changes, p(baseline, live)...)
	}
	return changes
}
