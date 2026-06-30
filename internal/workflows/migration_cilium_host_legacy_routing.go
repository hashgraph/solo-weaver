// SPDX-License-Identifier: Apache-2.0

// migration_cilium_host_legacy_routing.go sets enable-host-legacy-routing: "true"
// in the Cilium ConfigMap on already-provisioned clusters.
//
// PR #787 (#741) set bpf.hostLegacyRouting=true in the embedded cilium-config
// template so new installs pick up the setting automatically. This migration
// closes the gap for existing clusters: on the first provisioner run after
// upgrading across the 0.21.0 version boundary it re-renders cilium-config.yaml
// and runs `cilium upgrade` so the live cluster adopts enable-host-legacy-routing=true.
//
// The BN traffic shaper requires skb->priority to be preserved on the host
// legacy routing path. Cilium's Bandwidth Manager is the only other Cilium BPF
// writer of skb->priority, so the migration fails fast when Bandwidth Manager is
// enabled — keeping it on would void the traffic shaper's egress-priority guarantee.
//
// Registered in cmd/cli/commands/root.go RegisterMigrations() under
// migration.ScopeStartup.

package workflows

import (
	"context"
	"fmt"
	"strings"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// ciliumHostLegacyRoutingMinVersion is the CLI release that ships
// bpf.hostLegacyRouting=true in the embedded cilium-config template (PR #787)
// together with this migration. The migration applies when upgrading from a
// version below this boundary to this version or later.
const ciliumHostLegacyRoutingMinVersion = "0.21.0"

// hostLegacyRoutingConfigKey is the cilium-config data key rendered from
// bpf.hostLegacyRouting. "true" enables host legacy routing; absent or any other
// value disables it.
const hostLegacyRoutingConfigKey = "enable-host-legacy-routing"

// ciliumBandwidthManagerKey is the cilium-config data key for the Bandwidth
// Manager flag. Duplicated here (also defined in internal/workflows/steps) to
// keep the migration self-contained with no cross-package dependency.
const ciliumBandwidthManagerKey = "enable-bandwidth-manager"

// readHostLegacyRoutingState is a seam so tests can stub the ConfigMap read.
var readHostLegacyRoutingState = defaultReadHostLegacyRoutingState

// CiliumHostLegacyRoutingMigration re-renders cilium-config.yaml and runs
// `cilium upgrade` so already-provisioned clusters adopt
// enable-host-legacy-routing=true required by the BN traffic shaper.
type CiliumHostLegacyRoutingMigration struct {
	migration.CLIVersionMigration
}

// NewCiliumHostLegacyRoutingMigration constructs the migration.
func NewCiliumHostLegacyRoutingMigration() *CiliumHostLegacyRoutingMigration {
	return &CiliumHostLegacyRoutingMigration{
		CLIVersionMigration: migration.NewCLIVersionMigration(
			"cilium-host-legacy-routing-v"+ciliumHostLegacyRoutingMinVersion,
			"Re-render cilium-config and run 'cilium upgrade' so existing clusters adopt "+
				"enable-host-legacy-routing=true required by the BN traffic shaper",
			ciliumHostLegacyRoutingMinVersion,
		),
	}
}

// Execute re-renders cilium-config.yaml from the updated template and applies it
// with `cilium upgrade --values <config> --wait`.
//
// It is idempotent: it no-ops when the cluster is unreachable, when Cilium is not
// yet installed, or when enable-host-legacy-routing is already "true". It fails
// fast if Bandwidth Manager is enabled — Bandwidth Manager is the only Cilium BPF
// writer of skb->priority and its presence voids the traffic shaper's
// egress-priority guarantee.
func (m *CiliumHostLegacyRoutingMigration) Execute(ctx context.Context, mctx *migration.Context) error {
	if !kubernetesInstalled() {
		logx.As().Debug().Msg("cilium host-legacy-routing migration: Kubernetes not installed on this host; skipping")
		return nil
	}

	installed, hlr, bm, err := readHostLegacyRoutingState(ctx)
	if err != nil {
		logx.As().Debug().Err(err).
			Msg("cilium host-legacy-routing migration: Kubernetes API not reachable; skipping")
		return nil
	}
	if !installed {
		logx.As().Debug().Msg("cilium host-legacy-routing migration: Cilium not installed (no cilium-config); skipping")
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(bm), "true") {
		return errorx.IllegalState.New(
			"Cilium Bandwidth Manager is enabled (%s=%q) — it is the only Cilium BPF writer of skb->priority "+
				"and would void the BN traffic shaper's egress-priority guarantee; it must be disabled before this migration can run",
			ciliumBandwidthManagerKey, bm).
			WithProperty(models.ErrPropertyResolution, []string{
				"Disable the Cilium Bandwidth Manager, then re-run the provisioner:",
				"  set 'bandwidthManager.enabled: false' in the Cilium Helm values and run 'cilium upgrade'",
				"Verify with: kubectl -n kube-system get configmap cilium-config -o jsonpath='{.data.enable-bandwidth-manager}'",
			})
	}
	if strings.EqualFold(strings.TrimSpace(hlr), "true") {
		logx.As().Info().Str(hostLegacyRoutingConfigKey, hlr).
			Msg("cilium host-legacy-routing migration: nothing to do (already enabled)")
		return nil
	}

	configPath, err := reconfigureCiliumConfig()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to re-render cilium-config.yaml")
	}

	logx.As().Info().
		Str("config", configPath).
		Str("from", hlr).
		Str("to", "true").
		Msg("Applying cilium-config (enable-host-legacy-routing=true) to existing cluster via 'cilium upgrade'")

	upgrade := []string{
		fmt.Sprintf("/usr/bin/sudo %s/cilium upgrade --values %s --wait",
			models.Paths().SandboxBinDir, configPath),
	}
	if _, err := runShell(upgrade, ""); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to run 'cilium upgrade' to apply host-legacy-routing")
	}

	return nil
}

// Rollback is a no-op: re-running a known-good provisioner version re-applies the
// desired template. We intentionally do not revert host-legacy-routing — it is
// the required posture for the BN traffic shaper.
func (m *CiliumHostLegacyRoutingMigration) Rollback(ctx context.Context, mctx *migration.Context) error {
	logx.As().Warn().Msg("Rollback for cilium host-legacy-routing migration is not supported")
	return nil
}

// defaultReadHostLegacyRoutingState reports whether Cilium is installed (its
// cilium-config ConfigMap exists) and, if so, the enable-host-legacy-routing and
// enable-bandwidth-manager values from the ConfigMap. A non-nil error means the
// cluster/API is not reachable.
func defaultReadHostLegacyRoutingState(ctx context.Context) (installed bool, hostLegacyRouting string, bandwidthManager string, err error) {
	kc, err := kube.NewClient()
	if err != nil {
		return false, "", "", err
	}

	exists, err := kc.ResourceExists(ctx, "v1", "ConfigMap", "kube-system", "cilium-config")
	if err != nil {
		return false, "", "", err
	}
	if !exists {
		return false, "", "", nil
	}

	hlr, err := kc.GetResourceNestedString(ctx, "v1", "ConfigMap", "kube-system", "cilium-config", "data", hostLegacyRoutingConfigKey)
	if err != nil {
		return true, "", "", err
	}

	bm, err := kc.GetResourceNestedString(ctx, "v1", "ConfigMap", "kube-system", "cilium-config", "data", ciliumBandwidthManagerKey)
	if err != nil {
		return true, hlr, "", err
	}

	return true, hlr, bm, nil
}
