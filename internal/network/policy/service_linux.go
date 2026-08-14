// SPDX-License-Identifier: Apache-2.0

//go:build linux

package policy

import (
	"bytes"
	"context"
	"os"
	"path/filepath"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/templates"
	soos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/joomcode/errorx"
)

// defaultEnsureService installs the shared network-nft unit and enables it for
// boot. The unit is shared with internal/network/firewall; installing it here is
// idempotent with firewall's own install so whichever scope runs first wins.
//
// The on-disk unit is compared against the embedded copy rather than merely
// stat-ed, so a host provisioned by an older release converges on the current
// unit the next time either plane runs. Stat-and-skip would have stranded those
// hosts on whatever unit shipped when they were first provisioned.
//
// This package does NOT restart the unit to apply — `network policy` applies to
// the live kernel directly via `nft -f` (Runner.Apply). The unit only matters
// for boot replay.
func defaultEnsureService(ctx context.Context) error {
	content, err := templates.Files.ReadFile(networkNftServiceTemplate)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to read embedded %s", networkNftServiceTemplate)
	}

	if current, err := os.ReadFile(NetworkNftServiceUnitPath); err == nil && bytes.Equal(current, content) {
		return nil // already installed by firewall or a prior policy call
	}

	if err := os.MkdirAll(filepath.Dir(NetworkNftServiceUnitPath), 0o755); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to create %s", filepath.Dir(NetworkNftServiceUnitPath))
	}
	if err := os.WriteFile(NetworkNftServiceUnitPath, content, 0o644); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to write %s", NetworkNftServiceUnitPath)
	}

	if err := soos.DaemonReload(ctx); err != nil {
		return err
	}
	if err := soos.EnableService(ctx, NetworkNftService); err != nil {
		logx.As().Warn().Err(err).Str("service", NetworkNftService).Msg("could not enable service at boot")
	}
	return nil
}
