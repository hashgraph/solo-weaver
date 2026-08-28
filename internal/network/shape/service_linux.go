// SPDX-License-Identifier: Apache-2.0

//go:build linux

package shape

import (
	"context"
	"os"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/network/unitconv"
	"github.com/hashgraph/solo-weaver/internal/templates"
	soos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/joomcode/errorx"
)

// EnsureTcEgressUnit installs the embedded bandwidth-shaper unit at
// TcEgressServiceUnitPath, daemon-reloads, and enables it for boot.
// See unitconv.EnsureUnit for the convergence rules (#980).
func EnsureTcEgressUnit(ctx context.Context) error {
	content, err := templates.Files.ReadFile(tcEgressServiceTemplate)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to read embedded %s", tcEgressServiceTemplate)
	}
	return unitconv.EnsureUnit(ctx, TcEgressServiceUnitPath, TcEgressService, content)
}

// RemoveTcEgressUnit is the teardown counterpart to EnsureTcEgressUnit: it
// removes the boot script, then stops, disables and removes the unit, then
// daemon-reloads. Callers must tear the egress hierarchy down first — this
// removes only the boot-replay machinery, not the live tc state.
//
// The script is removed first because it is the guard the startup migration
// keys on; see docs/dev/traffic-shaper.md. Idempotent.
func RemoveTcEgressUnit(ctx context.Context) error {
	_, statErr := os.Stat(TcEgressServiceUnitPath)
	unitPresent := statErr == nil

	// Removed even when the unit is already gone, so no orphaned script is left.
	if err := os.Remove(TcEgressScriptPath); err != nil && !os.IsNotExist(err) {
		return errorx.ExternalError.Wrap(err, "failed to remove boot script %s", TcEgressScriptPath)
	}

	if !unitPresent {
		return nil
	}

	if running, _ := soos.IsServiceRunning(ctx, TcEgressService); running {
		if err := soos.StopService(ctx, TcEgressService); err != nil {
			logx.As().Warn().Err(err).Str("service", TcEgressService).
				Msg("could not stop bandwidth-shaper service; continuing teardown")
		}
	}
	if err := soos.DisableService(ctx, TcEgressService); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to disable %s", TcEgressService)
	}
	if err := os.Remove(TcEgressServiceUnitPath); err != nil && !os.IsNotExist(err) {
		return errorx.ExternalError.Wrap(err, "failed to remove unit file %s", TcEgressServiceUnitPath)
	}
	return soos.DaemonReload(ctx)
}
