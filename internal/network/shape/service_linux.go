// SPDX-License-Identifier: Apache-2.0

//go:build linux

package shape

import (
	"context"
	"crypto/sha256"
	"os"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/templates"
	soos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/joomcode/errorx"
)

// EnsureTcEgressUnit installs (or updates) the solo-provisioner-bandwidth-shaper.service
// unit file, daemon-reloads systemd, and enables the unit for boot. SHA-256
// comparison is used so the write, reload, and enable are skipped when the
// on-disk content already matches the embedded template — making repeated installs
// cheap while ensuring a template change (e.g. ordering fix) is applied
// automatically on the next `block node install`.
func EnsureTcEgressUnit(ctx context.Context) error {
	content, err := templates.Files.ReadFile(tcEgressServiceTemplate)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to read embedded %s", tcEgressServiceTemplate)
	}

	// Skip write + reload when the on-disk file is already identical.
	if existing, readErr := os.ReadFile(TcEgressServiceUnitPath); readErr == nil {
		if sha256.Sum256(content) == sha256.Sum256(existing) {
			return nil
		}
	}

	if err := atomicWriteFile(TcEgressServiceUnitPath, string(content), 0o644); err != nil {
		return err
	}
	if err := soos.DaemonReload(ctx); err != nil {
		return err
	}
	if err := soos.EnableService(ctx, TcEgressService); err != nil {
		logx.As().Warn().Err(err).Str("service", TcEgressService).Msg("could not enable bandwidth-shaper service at boot")
	}
	return nil
}

// RemoveTcEgressUnit is the teardown counterpart to EnsureTcEgressUnit: it
// stops and disables solo-provisioner-bandwidth-shaper.service, removes the unit
// file and the boot script it executes, then daemon-reloads. Callers must have
// torn the egress hierarchy down first — this only removes the boot-replay
// machinery, not the live tc state.
//
// Idempotent: an already-absent unit or script is not an error, and a stop that
// fails on an inactive oneshot is logged and ignored.
func RemoveTcEgressUnit(ctx context.Context) error {
	_, statErr := os.Stat(TcEgressServiceUnitPath)
	unitPresent := statErr == nil

	if unitPresent {
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
	}

	// Remove the boot script even when the unit was already gone, so a partial
	// teardown does not leave an orphaned script behind.
	if err := os.Remove(TcEgressScriptPath); err != nil && !os.IsNotExist(err) {
		return errorx.ExternalError.Wrap(err, "failed to remove boot script %s", TcEgressScriptPath)
	}

	if !unitPresent {
		return nil
	}
	return soos.DaemonReload(ctx)
}
