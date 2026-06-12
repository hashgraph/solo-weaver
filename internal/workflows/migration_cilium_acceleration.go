// SPDX-License-Identifier: Apache-2.0

// migration_cilium_acceleration.go re-applies the Cilium load-balancer
// acceleration setting to already-provisioned clusters.
//
// PR #674 changed the embedded cilium-config template to
// loadBalancer.acceleration: "disabled" (tc-eBPF) so Cilium no longer attaches a
// native XDP program to the host's public NIC. On ixgbe NICs (e.g. Intel X550)
// native XDP attach/reconcile resets the PHY and flaps the link, dropping the
// node's public IP (#669).
//
// The Cilium setup steps are guarded — configureCilium skips on the persisted
// IsConfigured flag, and installCiliumCNI skips when `cilium status` succeeds —
// so an already-provisioned cluster never re-renders or re-applies the template
// and keeps best-effort native XDP. This startup migration closes that gap: on
// the first provisioner run after upgrading across the version boundary it
// re-renders cilium-config.yaml and runs `cilium upgrade` so the live cluster
// adopts the disabled acceleration.
//
// Registered in cmd/cli/commands/root.go RegisterMigrations() under
// migration.ScopeStartup.

package workflows

import (
	"context"
	"fmt"

	"github.com/automa-saga/automa/automa_steps"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/joomcode/errorx"
)

// ciliumAccelerationMinVersion is the CLI release that ships the disabled-acceleration
// template (#674) together with this migration. The migration applies when
// upgrading from a version below this boundary to this version or later.
const ciliumAccelerationMinVersion = "0.19.1"

// disabledAcceleration is the target bpf-lb-acceleration value (tc-eBPF, no XDP).
const disabledAcceleration = "disabled"

// Seams so tests can stub the live-config read, the on-disk re-render, and shell
// execution without touching a cluster or the filesystem.
var (
	runShell                = automa_steps.RunBashScript
	reconfigureCiliumConfig = software.ReconfigureCiliumConfig
	readCiliumAcceleration  = liveCiliumAcceleration
)

// CiliumAccelerationMigration re-renders cilium-config.yaml and runs `cilium upgrade`
// so already-provisioned clusters adopt loadBalancer.acceleration=disabled (#669/#674).
type CiliumAccelerationMigration struct {
	migration.CLIVersionMigration
}

// NewCiliumAccelerationMigration constructs the migration.
func NewCiliumAccelerationMigration() *CiliumAccelerationMigration {
	return &CiliumAccelerationMigration{
		CLIVersionMigration: migration.NewCLIVersionMigration(
			"cilium-disable-xdp-acceleration-v"+ciliumAccelerationMinVersion,
			"Re-render cilium-config and run 'cilium upgrade' so existing clusters adopt "+
				"loadBalancer.acceleration=disabled (tc-eBPF) instead of best-effort native XDP (#669)",
			ciliumAccelerationMinVersion,
		),
	}
}

// Execute re-renders cilium-config.yaml from the updated template and applies it
// with `cilium upgrade --values <config> --wait`.
//
// It is self-guarding and idempotent: it no-ops when no live cilium-config is
// reachable (the node has no cluster) or when acceleration is already "disabled"
// (the migration already ran). Applies() only gates on the CLI version boundary,
// so these checks make repeated invocations within the upgrade window harmless.
func (m *CiliumAccelerationMigration) Execute(ctx context.Context, mctx *migration.Context) error {
	acc, err := readCiliumAcceleration(ctx)
	if err != nil {
		logx.As().Debug().Err(err).
			Msg("cilium acceleration migration: cilium-config not reachable (no cluster on this node?); skipping")
		return nil
	}
	if acc == "" || acc == disabledAcceleration {
		logx.As().Info().Str("acceleration", acc).
			Msg("cilium acceleration migration: nothing to do (no cilium-config or already disabled)")
		return nil
	}

	configPath, err := reconfigureCiliumConfig()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to re-render cilium-config.yaml")
	}

	logx.As().Info().
		Str("config", configPath).
		Str("from", acc).
		Str("to", disabledAcceleration).
		Msg("Applying cilium-config (loadBalancer.acceleration=disabled) to existing cluster via 'cilium upgrade'")

	upgrade := []string{
		fmt.Sprintf("/usr/bin/sudo %s/cilium upgrade --values %s --wait",
			models.Paths().SandboxBinDir, configPath),
	}
	if _, err := runShell(upgrade, ""); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to run 'cilium upgrade' to apply disabled LB acceleration")
	}

	return nil
}

// Rollback is a no-op: re-running a known-good provisioner version re-applies the
// desired template. We intentionally do not restore best-effort acceleration —
// that is the NIC-flapping behaviour the migration exists to remove.
func (m *CiliumAccelerationMigration) Rollback(ctx context.Context, mctx *migration.Context) error {
	logx.As().Warn().Msg("Rollback for cilium acceleration migration is not supported (would re-introduce the NIC-flapping XDP acceleration)")
	return nil
}

// liveCiliumAcceleration reads bpf-lb-acceleration from the live cilium-config
// ConfigMap via the Kubernetes API (the provisioner's own client, which resolves
// the in-cluster config or the admin kubeconfig). Returns ("", nil) when the
// ConfigMap is absent, and ("", err) when the cluster is not reachable.
func liveCiliumAcceleration(ctx context.Context) (string, error) {
	kc, err := kube.NewClient()
	if err != nil {
		return "", err
	}
	return kc.GetResourceNestedString(ctx, "v1", "ConfigMap", "kube-system", "cilium-config", "data", "bpf-lb-acceleration")
}
