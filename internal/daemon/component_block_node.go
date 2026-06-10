// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"context"

	"github.com/automa-saga/logx"
)

// blockNodeUpgradeMonitor is a stub implementation of MonitorRunner for the
// block-node upgrade workflow. The real implementation is blocked on the
// block-node operator CR type spec and will land in a follow-up story.
//
// It blocks on ctx to keep the supervised goroutine alive, logging once on
// start so operators can confirm the component is running.
type blockNodeUpgradeMonitor struct{}

// Name implements MonitorRunner.
func (m *blockNodeUpgradeMonitor) Name() string { return "bn-upgrade-monitor" }

// Run implements MonitorRunner. Blocks until ctx is cancelled.
func (m *blockNodeUpgradeMonitor) Run(ctx context.Context) error {
	logx.As().Info().
		Str("reason", "MonitorStubRunning").
		Str("monitor", m.Name()).
		Msg("block-node upgrade monitor not yet implemented — stub running")
	<-ctx.Done()
	return nil
}
