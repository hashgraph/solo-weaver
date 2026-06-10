// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/daemon/core"
)

// upgradeMonitor is a stub implementation of core.MonitorRunner for the
// block-node upgrade workflow. The real implementation is blocked on the
// block-node operator CR type spec and will land in a follow-up story.
//
// It blocks on ctx to keep the supervised goroutine alive, logging once on
// start so operators can confirm the component is running.
//
// NOTE: it is just a stubbed item, can be removed later if not needed
type upgradeMonitor struct{}

// Name implements core.MonitorRunner.
func (m *upgradeMonitor) Name() string { return "bn-upgrade-monitor" }

// Run implements core.MonitorRunner. Blocks until ctx is cancelled.
func (m *upgradeMonitor) Run(ctx context.Context) error {
	logx.As().Info().
		Str("reason", "MonitorStubRunning").
		Str("monitor", m.Name()).
		Msg("block-node upgrade monitor not yet implemented — stub running")
	<-ctx.Done()
	return nil
}

// ComponentConfig holds inputs needed to build the block-node component.
type ComponentConfig struct {
	UpgradeEnabled bool
}

// ComponentResult contains the monitors built by NewComponent.
type ComponentResult struct {
	Monitors []core.MonitorRunner
}

// NewComponent constructs all enabled monitors for the block-node component.
func NewComponent(cfg ComponentConfig) (ComponentResult, error) {
	var monitors []core.MonitorRunner
	if cfg.UpgradeEnabled {
		monitors = append(monitors, &upgradeMonitor{})
	}
	return ComponentResult{Monitors: monitors}, nil
}
