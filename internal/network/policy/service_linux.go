// SPDX-License-Identifier: Apache-2.0

//go:build linux

package policy

import (
	"context"
	"os"
	"path/filepath"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/templates"
	soos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/joomcode/errorx"
)

// defaultEnsureService installs the shared network-nft unit if absent and
// enables it for boot (design §8.4.5: "First call also … enables the systemd
// unit"). The unit is shared with internal/network/firewall; installing it here
// is idempotent with firewall's own install so whichever scope runs first wins.
//
// This package does NOT restart the unit to apply — `network policy` applies to
// the live kernel directly via `nft -f` (Runner.Apply). The unit only matters
// for boot replay, and extending its ExecStart to load network-weaver.nft is
// owned by #780.
func defaultEnsureService(ctx context.Context) error {
	if _, err := os.Stat(NetworkNftServiceUnitPath); err == nil {
		return nil // already installed by firewall or a prior policy call
	}

	content, err := templates.Files.ReadFile(networkNftServiceTemplate)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to read embedded %s", networkNftServiceTemplate)
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
