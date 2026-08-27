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

// SyncDNSRefreshTimer installs and enables the DNS refresh timer when the
// management allowlist holds at least one domain name, and disables and removes
// it when it does not.
//
// Driven by the config's content rather than installed once at provision time,
// so a host whose allowlist is all literals never carries a timer that would
// wake every five minutes to re-render an identical ruleset — and so removing
// the last name actually stops the refresh instead of leaving it running
// against nothing.
//
// Byte-compares before writing, for the same reason EnsureNetworkNftUnit does.
func SyncDNSRefreshTimer(ctx context.Context, wanted bool) error {
	if !wanted {
		return removeDNSRefreshTimer(ctx)
	}

	changed := false
	for _, u := range []struct{ template, path string }{
		{dnsRefreshServiceTemplate, DNSRefreshServiceUnitPath},
		{dnsRefreshTimerTemplate, DNSRefreshTimerUnitPath},
	} {
		content, err := templates.Files.ReadFile(u.template)
		if err != nil {
			return errorx.InternalError.Wrap(err, "failed to read embedded %s", u.template)
		}
		if current, err := os.ReadFile(u.path); err == nil && bytes.Equal(current, content) {
			continue
		}
		if err := writeEmbedded(content, u.path); err != nil {
			return err
		}
		changed = true
	}

	if changed {
		if err := soos.DaemonReload(ctx); err != nil {
			return err
		}
	}

	// Enabling is idempotent, so it runs even when nothing was rewritten: an
	// operator who disabled the timer by hand gets it back on the next mutation,
	// which is the same convergence rule the nft unit follows.
	if err := soos.EnableService(ctx, DNSRefreshTimer); err != nil {
		logx.As().Warn().Err(err).Str("unit", DNSRefreshTimer).Msg(
			"could not enable the DNS refresh timer; management allowlist names will not follow address changes until it is enabled")
		return nil
	}
	return soos.StartService(ctx, DNSRefreshTimer)
}

// removeDNSRefreshTimer stops, disables and deletes both units. Every step is
// best-effort: this runs on the way to a successful apply, and a leftover unit
// must not fail a firewall change.
func removeDNSRefreshTimer(ctx context.Context) error {
	if _, err := os.Stat(DNSRefreshTimerUnitPath); os.IsNotExist(err) {
		return nil
	}
	if err := soos.StopService(ctx, DNSRefreshTimer); err != nil {
		logx.As().Debug().Err(err).Str("unit", DNSRefreshTimer).Msg("could not stop the DNS refresh timer")
	}
	if err := soos.DisableService(ctx, DNSRefreshTimer); err != nil {
		logx.As().Debug().Err(err).Str("unit", DNSRefreshTimer).Msg("could not disable the DNS refresh timer")
	}
	for _, p := range []string{DNSRefreshTimerUnitPath, DNSRefreshServiceUnitPath} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			logx.As().Warn().Err(err).Str("path", p).Msg("could not remove the DNS refresh unit")
		}
	}
	return soos.DaemonReload(ctx)
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
