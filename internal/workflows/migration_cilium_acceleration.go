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
	"os"

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

// adminKubeconfigPath is the kubeadm-written admin kubeconfig; its presence is the
// marker that Kubernetes has been provisioned on this host.
const adminKubeconfigPath = "/etc/kubernetes/admin.conf"

// Seams so tests can stub the install preconditions, the on-disk re-render, and
// shell execution without touching a cluster or the filesystem.
var (
	runShell                = automa_steps.RunBashScript
	reconfigureCiliumConfig = software.ReconfigureCiliumConfig
	kubernetesInstalled     = defaultKubernetesInstalled
	readCiliumState         = defaultReadCiliumState
	verifyCiliumExecutable  = func() error { return software.VerifyExecutables("cilium") }
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
	// The provisioner can run before anything is deployed, so reconfigure only
	// when both Kubernetes and Cilium are actually installed on this host.
	if !kubernetesInstalled() {
		logx.As().Debug().Msg("cilium acceleration migration: Kubernetes not installed on this host; skipping")
		return nil
	}

	installed, acc, err := readCiliumState(ctx)
	if err != nil {
		logx.As().Debug().Err(err).
			Msg("cilium acceleration migration: Kubernetes API not reachable; skipping")
		return nil
	}
	if !installed {
		logx.As().Debug().Msg("cilium acceleration migration: Cilium not installed (no cilium-config); skipping")
		return nil
	}
	if acc == disabledAcceleration || acc == "" {
		logx.As().Info().Str("acceleration", acc).
			Msg("cilium acceleration migration: nothing to do (already disabled)")
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

	// Verify the cilium binary before executing it.
	if err := verifyCiliumExecutable(); err != nil {
		return err
	}

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

// KubernetesInstalled reports whether Kubernetes has been provisioned on this
// host, using the same probe the Cilium startup migrations gate their Execute()
// on. The startup orchestrator uses it to decide whether to record the
// provisioner version after a migration pass, so a genuinely fresh machine keeps
// having no state file.
func KubernetesInstalled() bool { return kubernetesInstalled() }

// defaultKubernetesInstalled reports whether Kubernetes has been provisioned on
// this host (the kubeadm admin kubeconfig exists).
func defaultKubernetesInstalled() bool {
	_, err := os.Stat(adminKubeconfigPath)
	return err == nil
}

// defaultReadCiliumState reports whether Cilium is installed (its cilium-config
// ConfigMap exists) and, if so, its bpf-lb-acceleration value. It uses the
// provisioner's own Kubernetes client (resolving the in-cluster config or admin
// kubeconfig). A non-nil error means the cluster/API is not reachable.
func defaultReadCiliumState(ctx context.Context) (installed bool, acceleration string, err error) {
	kc, err := kube.NewClient()
	if err != nil {
		return false, "", err
	}

	exists, err := kc.ResourceExists(ctx, "v1", "ConfigMap", "kube-system", "cilium-config")
	if err != nil {
		return false, "", err
	}
	if !exists {
		return false, "", nil
	}

	acc, err := kc.GetResourceNestedString(ctx, "v1", "ConfigMap", "kube-system", "cilium-config", "data", "bpf-lb-acceleration")
	if err != nil {
		return true, "", err
	}
	return true, acc, nil
}
