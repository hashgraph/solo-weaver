// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"context"

	"github.com/automa-saga/logx"
)

// UpgradeMonitor watches the K8s API for NetworkUpgradeExecute CR status
// ReadyForProvisionerDaemon and triggers the execute-phase workflow.
// Stub — implemented in story #519.
type UpgradeMonitor struct{}

func NewUpgradeMonitor() *UpgradeMonitor { return &UpgradeMonitor{} }

// Run blocks until ctx is cancelled. Implemented in story #519.
func (um *UpgradeMonitor) Run(ctx context.Context) error {
	logx.As().Info().Str("reason", "UpgradeMonitorStarted").Msg("Upgrade monitor started")
	<-ctx.Done()
	logx.As().Info().Str("reason", "UpgradeMonitorStopped").Msg("Upgrade monitor stopped")
	return nil
}
