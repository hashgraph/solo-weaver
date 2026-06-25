// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"github.com/automa-saga/daemonkit"
)

// ComponentConfig holds inputs needed to build the block-node component.
type ComponentConfig struct {
	TrafficShaperEnabled bool
}

// ComponentResult contains the monitors built by NewComponent.
type ComponentResult struct {
	Monitors []daemonkit.MonitorRunner
}

// NewComponent constructs all enabled monitors for the block-node component.
func NewComponent(cfg ComponentConfig) (ComponentResult, error) {
	var monitors []daemonkit.MonitorRunner
	if cfg.TrafficShaperEnabled {
		monitors = append(monitors, &trafficShaperMonitor{})
	}
	return ComponentResult{Monitors: monitors}, nil
}
