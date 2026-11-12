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
// IsConfigured flag, and installCiliumCNI skips when Cilium is already deployed —
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
	"time"

	"github.com/automa-saga/automa/automa_steps"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/cilium"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
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
// marker that Kubernetes has been provisioned on this host. The Cilium
// migrations read and change the cluster through it alone. A var so tests can
// point it at a fake API server.
var adminKubeconfigPath = "/etc/kubernetes/admin.conf"

// ciliumAPITimeout bounds each request to the API server, so a hung one defers
// the migration instead of stalling the command. A var so tests can shorten it.
var ciliumAPITimeout = 5 * time.Second

// Seams so tests can stub the install preconditions, the on-disk re-render, and
// shell execution without touching a cluster or the filesystem.
var (
	runShell                = automa_steps.RunBashScript
	reconfigureCiliumConfig = software.ReconfigureCiliumConfig
	kubernetesInstalled     = defaultKubernetesInstalled
	readCiliumState         = defaultReadCiliumState
	verifyCiliumExecutable  = func() error { return software.VerifyExecutables("cilium") }
	installedCiliumVersion  = defaultInstalledCiliumVersion
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
// with `cilium upgrade --version <deployed> --values <config> --wait`.
//
// It is self-guarding and idempotent: it no-ops when the host has no cluster or
// no cilium-config, or when acceleration is already "disabled" (the migration
// already ran), and it defers when the Kubernetes API cannot be read. Applies()
// only gates on the CLI version boundary, so these checks make repeated
// invocations within the upgrade window harmless.
func (m *CiliumAccelerationMigration) Execute(ctx context.Context, mctx *migration.Context) error {
	// The provisioner can run before anything is deployed, so reconfigure only
	// when both Kubernetes and Cilium are actually installed on this host.
	if !kubernetesInstalled() {
		logx.As().Debug().Msg("cilium acceleration migration: Kubernetes not installed on this host; skipping")
		return nil
	}

	installed, acc, err := readCiliumState(ctx)
	if err != nil {
		return deferIfAPIDown(err)
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

	version, err := prepareCiliumUpgrade(ctx)
	if err != nil {
		return deferIfAPIDown(err)
	}

	configPath, err := reconfigureCiliumConfig()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to re-render cilium-config.yaml")
	}

	logx.As().Info().
		Str("config", configPath).
		Str("ciliumVersion", version).
		Str("from", acc).
		Str("to", disabledAcceleration).
		Msg("Applying cilium-config (loadBalancer.acceleration=disabled) to existing cluster via 'cilium upgrade'")

	if _, err := runShell(ciliumUpgradeCommand(version, configPath), ""); err != nil {
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

// KubernetesInstalled reports whether Kubernetes is provisioned on this host,
// using the same probe the Cilium migrations gate Execute() on. The startup
// orchestrator uses it to decide whether to record the provisioner version, so a
// fresh machine keeps having no state file. A var so tests can stub the probe
// (which reads /etc/kubernetes/admin.conf, absent on a CI host).
var KubernetesInstalled = defaultKubernetesInstalled

// defaultKubernetesInstalled reports whether Kubernetes has been provisioned on
// this host (the kubeadm admin kubeconfig exists).
func defaultKubernetesInstalled() bool {
	_, err := os.Stat(adminKubeconfigPath)
	return err == nil
}

// defaultReadCiliumState reports whether Cilium is installed (its cilium-config
// ConfigMap exists) and, if so, its bpf-lb-acceleration value, read through
// adminKubeconfigPath. Any error reading the cluster is tagged K8sAPIFailed.
func defaultReadCiliumState(ctx context.Context) (installed bool, acceleration string, err error) {
	kc, err := adminClient()
	if err != nil {
		return false, "", err
	}

	exists, err := kc.ResourceExists(ctx, "v1", "ConfigMap", "kube-system", "cilium-config")
	if err != nil {
		return false, "", clusterAPIError(err, "cannot read kube-system/cilium-config")
	}
	if !exists {
		return false, "", nil
	}

	acc, err := kc.GetResourceNestedString(ctx, "v1", "ConfigMap", "kube-system", "cilium-config", "data", "bpf-lb-acceleration")
	if err != nil {
		return true, "", clusterAPIError(err, "cannot read kube-system/cilium-config")
	}
	return true, acc, nil
}

// adminClient is a client for the cluster behind adminKubeconfigPath, with
// every request bounded by ciliumAPITimeout.
func adminClient() (*kube.Client, error) {
	kc, err := kube.NewClient(kube.WithKubeconfig(adminKubeconfigPath), kube.WithRequestTimeout(ciliumAPITimeout))
	if err != nil {
		return nil, clusterAPIError(err, "cannot load %s", adminKubeconfigPath)
	}
	return kc, nil
}

// clusterAPIError tags an error from the API server behind adminKubeconfigPath,
// so the migration defers with what to check.
func clusterAPIError(err error, format string, args ...any) error {
	return errx.Decorate(errorx.ExternalError.Wrap(err, format, args...), reasons.K8sAPIFailed,
		apiServerHint(),
		"Check the admin credentials: sudo kubectl --kubeconfig "+adminKubeconfigPath+" auth can-i get configmaps -n kube-system")
}

// apiServerHint is how to check the API server behind adminKubeconfigPath.
func apiServerHint() string {
	return "Check the API server: sudo kubectl --kubeconfig " + adminKubeconfigPath + " get --raw /readyz"
}

// deferIfAPIDown defers a Cilium migration whose error, met before it changed
// anything, came from the Kubernetes API; any other error still fails it.
func deferIfAPIDown(err error) error {
	if apiDown(err) {
		return migration.ErrDeferred.Wrap(err, "the Kubernetes API is not available")
	}
	return err
}

// apiDown reports whether err came from the Kubernetes API.
func apiDown(err error) bool {
	reason, _ := errx.ReasonOf(err)
	return reason == reasons.K8sAPIFailed
}

// prepareCiliumUpgrade checks what a Cilium migration needs before it touches
// anything: the deployed chart version to pin, and a verified cilium binary.
// Without --version, `cilium upgrade` moves the cluster to the CLI's own default
// release, so a newer CLI would silently upgrade the agent.
func prepareCiliumUpgrade(ctx context.Context) (string, error) {
	version, err := installedCiliumVersion(ctx)
	if err != nil {
		return "", errorx.Decorate(err, "cannot determine the deployed Cilium version, so 'cilium upgrade' was not run")
	}

	if err := verifyCiliumExecutable(); err != nil {
		return "", err
	}

	return version, nil
}

// ciliumUpgradeCommand pins the upgrade to version so it changes values only,
// and points the cilium CLI at the admin kubeconfig the reads used.
func ciliumUpgradeCommand(version, configPath string) []string {
	return []string{
		fmt.Sprintf("/usr/bin/sudo %s/cilium upgrade --kubeconfig %s --version %q --values %s --wait",
			models.Paths().SandboxBinDir, adminKubeconfigPath, version, configPath),
	}
}

func defaultInstalledCiliumVersion(ctx context.Context) (string, error) {
	kc, err := adminClient()
	if err != nil {
		return "", err
	}
	version, err := cilium.InstalledVersion(ctx, kc.Dyn)
	if apiDown(err) {
		// Its hints use the default kubeconfig; this read went through admin.conf.
		return "", errx.WithHints(err, apiServerHint(),
			"Check the admin credentials: sudo kubectl --kubeconfig "+adminKubeconfigPath+" auth can-i get daemonsets -n kube-system")
	}
	return version, err
}
