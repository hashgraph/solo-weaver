// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"

	"github.com/automa-saga/logx"
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
