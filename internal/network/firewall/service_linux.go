// SPDX-License-Identifier: Apache-2.0

//go:build linux

package firewall

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

// defaultApplyViaService installs the network-nft unit file if absent, enables
// it for boot, and restarts it so the kernel picks up the just-written
// network-weaver-host-firewall.nft immediately — all via DBus, no nft exec for the apply path.
func defaultApplyViaService(ctx context.Context) error {
	if err := EnsureNetworkNftUnit(ctx); err != nil {
		return err
	}
	return soos.RestartService(ctx, NetworkNftService)
}

// EnsureNetworkNftUnit writes the embedded service unit file to
// NetworkNftServiceUnitPath, then daemon-reloads and enables the unit for boot.
//
// The on-disk unit is compared against the embedded copy, not merely stat-ed, so
// an already-provisioned host converges on the current unit the next time a
// mutation runs. Stat-and-skip would have stranded every existing host on the
// unit that shipped when it was first provisioned — including the missing
// StartLimitIntervalSec=0 that lets a run of failed applies wedge every later
// command behind systemd's start limit (#1002). An unchanged unit is still a
// fast no-op: no write, no daemon-reload.
func EnsureNetworkNftUnit(ctx context.Context) error {
	content, err := templates.Files.ReadFile(networkNftServiceTemplate)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to read embedded %s", networkNftServiceTemplate)
	}

	if current, err := os.ReadFile(NetworkNftServiceUnitPath); err == nil && bytes.Equal(current, content) {
		return nil // already installed and current — fast path
	}

	if err := writeEmbedded(content, NetworkNftServiceUnitPath); err != nil {
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

func writeEmbedded(content []byte, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to create %s", filepath.Dir(destPath))
	}
	if err := os.WriteFile(destPath, content, 0o644); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to write %s", destPath)
	}
	return nil
}
