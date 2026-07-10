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
	if err := EnsureNetworkNftUnit(ctx); err != nil {
		return err
	}
	return soos.RestartService(ctx, NetworkNftService)
}

// EnsureNetworkNftUnit writes the embedded service unit file to
// NetworkNftServiceUnitPath if it is absent, then daemon-reloads and enables
// the unit for boot. Stat-and-skip so repeated calls are a fast no-op.
func EnsureNetworkNftUnit(ctx context.Context) error {
	if _, err := os.Stat(NetworkNftServiceUnitPath); err == nil {
		return nil // already installed — fast path
	}

	if err := writeEmbedded(networkNftServiceTemplate, NetworkNftServiceUnitPath); err != nil {
		return err
	}
	if err := soos.DaemonReload(ctx); err != nil {
		return err
	}
	if err := soos.EnableService(ctx, NetworkNftService); err != nil {
		logx.As().Warn().Err(err).Str("service", NetworkNftService).Msg("could not enable service at boot")
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
