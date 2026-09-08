// SPDX-License-Identifier: Apache-2.0

package network

import "github.com/automa-saga/daemonkit"

// ComponentConfig holds inputs needed to build the network component. Empty for
// now: what to check is decided per tick from the artifacts on disk.
type ComponentConfig struct{}

// ComponentResult contains the monitors built by NewComponent.
type ComponentResult struct {
	// Monitors is the ordered slice of monitors to run under the supervisor.
	Monitors []daemonkit.MonitorRunner

	// ReassertMonitor is the presence-check monitor, exposed for GET /status.
	ReassertMonitor *ReassertMonitor
}

// NewComponent constructs the network component's monitors. Nothing is gated
// here; the monitor decides per tick whether the host has anything to check.
func NewComponent(_ ComponentConfig) (ComponentResult, error) {
	mon := NewReassertMonitor()
	return ComponentResult{
		Monitors:        []daemonkit.MonitorRunner{mon},
		ReassertMonitor: mon,
	}, nil
}
