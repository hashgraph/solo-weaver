// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"github.com/automa-saga/daemonkit"
)

// ComponentConfig holds inputs needed to build the block-node component.
type ComponentConfig struct {
	TrafficShaperEnabled bool
}

// ComponentResult contains the monitors built by NewComponent and a reference
// to the TrafficShaperMonitor (when enabled) so daemon.go can wire the HTTP
// handler with the per-component StatusTracker closure after the component is
// assembled.
type ComponentResult struct {
	// Monitors is the ordered slice of monitors to run under the supervisor.
	Monitors []daemonkit.MonitorRunner

	// TrafficShaperMonitor is non-nil when the traffic-shaper monitor is enabled.
	// daemon.go uses this to construct BlockNodeHandler with the correct
	// trafficShaperStateFn closure after the component's StatusTracker is created.
	TrafficShaperMonitor *TrafficShaperMonitor
}

// NewComponent constructs all enabled monitors for the block-node component
// and returns them alongside any references needed for HTTP handler wiring.
func NewComponent(cfg ComponentConfig) (ComponentResult, error) {
	var monitors []daemonkit.MonitorRunner

	var tsm *TrafficShaperMonitor
	if cfg.TrafficShaperEnabled {
		tsm = NewTrafficShaperMonitor()
		monitors = append(monitors, tsm)
	}

	return ComponentResult{
		Monitors:             monitors,
		TrafficShaperMonitor: tsm,
	}, nil
}
