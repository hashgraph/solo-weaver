// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/daemon/core"
)

// trafficShaperMonitor is a stub implementation of core.MonitorRunner for the
// block-node traffic-shaper workflow.
//
// It blocks on ctx to keep the supervised goroutine alive, logging once on
// start so operators can confirm the component is running.
type trafficShaperMonitor struct{}

// Name implements core.MonitorRunner.
func (m *trafficShaperMonitor) Name() string { return "bn-traffic-shaper-monitor" }

// Run implements core.MonitorRunner. Blocks until ctx is cancelled.
func (m *trafficShaperMonitor) Run(ctx context.Context) error {
	logx.As().Info().
		Str("reason", "MonitorStubRunning").
		Str("monitor", m.Name()).
		Msg("block-node traffic-shaper monitor not yet implemented — stub running")
	<-ctx.Done()
	return nil
}

// ComponentConfig holds inputs needed to build the block-node component.
type ComponentConfig struct {
	TrafficShaperEnabled bool
}

// ComponentResult contains the monitors built by NewComponent.
type ComponentResult struct {
	Monitors []core.MonitorRunner
}

// NewComponent constructs all enabled monitors for the block-node component.
func NewComponent(cfg ComponentConfig) (ComponentResult, error) {
	var monitors []core.MonitorRunner
	if cfg.TrafficShaperEnabled {
		monitors = append(monitors, &trafficShaperMonitor{})
	}
	return ComponentResult{Monitors: monitors}, nil
}
