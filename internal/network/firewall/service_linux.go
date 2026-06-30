// SPDX-License-Identifier: Apache-2.0

//go:build linux

package firewall

import (
	"context"
	"os"
	"path/filepath"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/templates"
	soos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/joomcode/errorx"
)

// defaultApplyViaService installs the network-nft unit file if absent, enables
// it for boot, and restarts it so the kernel picks up the just-written
// network-host.nft immediately — all via DBus, no nft exec for the apply path.
func defaultApplyViaService(ctx context.Context) error {
	if err := ensureNetworkNftUnit(ctx); err != nil {
		return err
	}
	return soos.RestartService(ctx, NetworkNftService)
}

// ensureNetworkNftUnit writes the embedded service unit and the nftables
// drop-in on first call. Both are stat-and-skip independently so a partial
// install (e.g. upgrading from a version that predates the drop-in) is
// repaired on the next mutation without a full reinstall.
func ensureNetworkNftUnit(ctx context.Context) error {
	_, unitErr := os.Stat(NetworkNftServiceUnitPath)
	_, dropInErr := os.Stat(NftablesDropInPath)
	if unitErr == nil && dropInErr == nil {
		return nil // both already installed — fast path
	}

	if unitErr != nil {
		if err := writeEmbedded(networkNftServiceTemplate, NetworkNftServiceUnitPath); err != nil {
			return err
		}
	}
	if dropInErr != nil {
		if err := writeEmbedded(nftablesDropInTemplate, NftablesDropInPath); err != nil {
			return err
		}
	}

	if err := soos.DaemonReload(ctx); err != nil {
		return err
	}
	if unitErr != nil {
		// Only enable at boot on first install; the drop-in alone doesn't need it.
		if err := soos.EnableService(ctx, NetworkNftService); err != nil {
			logx.As().Warn().Err(err).Str("service", NetworkNftService).Msg("could not enable service at boot")
		}
	}
	return nil
}

func writeEmbedded(tmplPath, destPath string) error {
	content, err := templates.Files.ReadFile(tmplPath)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to read embedded %s", tmplPath)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to create %s", filepath.Dir(destPath))
	}
	if err := os.WriteFile(destPath, content, 0o644); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to write %s", destPath)
	}
	return nil
}
