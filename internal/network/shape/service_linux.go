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

// EnsureTcEgressUnit installs (or updates) the solo-provisioner-tc-egress.service
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
		logx.As().Warn().Err(err).Str("service", TcEgressService).Msg("could not enable tc-egress service at boot")
	}
	return nil
}
