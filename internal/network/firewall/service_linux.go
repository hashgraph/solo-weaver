// SPDX-License-Identifier: Apache-2.0

//go:build linux

package firewall

import (
	"bytes"
	"context"
	"os"
	"path/filepath"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/network/unitconv"
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

// EnsureNetworkNftUnit installs the embedded loader unit at
// NetworkNftServiceUnitPath, daemon-reloads, and enables it for boot.
// See unitconv.EnsureUnit for the convergence rules (#982, #1002).
func EnsureNetworkNftUnit(ctx context.Context) error {
	content, err := templates.Files.ReadFile(networkNftServiceTemplate)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to read embedded %s", networkNftServiceTemplate)
	}
	return unitconv.EnsureUnit(ctx, NetworkNftServiceUnitPath, NetworkNftService, content)
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
	// Warn-only, for the same reason EnableService is: the caller is
	// applyAndPersist, already past both artifact writes, and a failure here
	// must not be mistaken for a failure to apply the firewall itself.
	if err := soos.StartService(ctx, DNSRefreshTimer); err != nil {
		logx.As().Warn().Err(err).Str("unit", DNSRefreshTimer).Msg(
			"could not start the DNS refresh timer; management allowlist names will not follow address changes until it is restarted")
	}
	return nil
}

// removeDNSRefreshTimer stops, disables and deletes both units. Every step is
// best-effort: this runs on the way to a successful apply, and a leftover unit
// must not fail a firewall change.
//
// Skips only when NEITHER file is present. A crash between SyncDNSRefreshTimer's
// two writes (service first, timer second), or a manual removal of just one
// file, must not strand the other unit on disk forever.
func removeDNSRefreshTimer(ctx context.Context) error {
	_, errTimer := os.Stat(DNSRefreshTimerUnitPath)
	_, errService := os.Stat(DNSRefreshServiceUnitPath)
	if os.IsNotExist(errTimer) && os.IsNotExist(errService) {
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
