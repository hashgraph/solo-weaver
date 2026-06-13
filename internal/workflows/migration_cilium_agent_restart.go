// SPDX-License-Identifier: Apache-2.0

// migration_cilium_agent_restart.go restarts the Cilium agents so existing
// clusters actually apply loadBalancer.acceleration=disabled.
//
// The v0.19.1 migration (migration_cilium_acceleration.go) runs
// `cilium upgrade --values` to flip the cilium-config ConfigMap to
// acceleration=disabled, but the Cilium chart's agent pod template has no
// checksum/config annotation, so a ConfigMap-only upgrade does not roll the
// DaemonSet. The agent reads bpf-lb-acceleration only at startup, so the value is
// staged but never applied — XDP stays attached to the public NIC (#669).
//
// This is a SEPARATE migration gated on the v0.19.2 boundary so it also fires on
// hosts that already ran the v0.19.1 migration (installed == 0.19.1): those hosts
// have acceleration=disabled in the ConfigMap but a still-running agent holding
// the old XDP program. Registering it after CiliumAccelerationMigration means a
// 0.18.x -> 0.19.2 upgrade flips the config (0.19.1 migration) and then restarts
// the agents (this migration) in one pass.
//
// Registered in cmd/cli/commands/root.go RegisterMigrations() under
// migration.ScopeStartup.

package workflows

import (
	"context"
	"time"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/joomcode/errorx"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ciliumAgentRestartMinVersion is the CLI release that ships this migration. It
// applies when upgrading from a version below this boundary to it or later — in
// particular from 0.19.1 (config already flipped, agents not yet restarted).
const ciliumAgentRestartMinVersion = "0.19.2"

// Cilium agent DaemonSet coordinates and the rollout-wait budget.
const (
	ciliumNamespace      = "kube-system"
	ciliumDaemonSet      = "cilium"
	ciliumRolloutTimeout = 5 * time.Minute
)

// restartCiliumAgents is a seam so tests can stub the agent restart.
var restartCiliumAgents = defaultRestartCiliumAgents

// CiliumAgentRestartMigration restarts the Cilium agents so already-provisioned
// clusters apply loadBalancer.acceleration=disabled (detach XDP) after the
// v0.19.1 migration updated the ConfigMap but did not roll the DaemonSet (#669).
type CiliumAgentRestartMigration struct {
	migration.CLIVersionMigration
}

// NewCiliumAgentRestartMigration constructs the migration.
func NewCiliumAgentRestartMigration() *CiliumAgentRestartMigration {
	return &CiliumAgentRestartMigration{
		CLIVersionMigration: migration.NewCLIVersionMigration(
			"cilium-agent-restart-disabled-acceleration-v"+ciliumAgentRestartMinVersion,
			"Restart Cilium agents so existing clusters apply loadBalancer.acceleration=disabled "+
				"(detach XDP); the v0.19.1 migration updated the ConfigMap but the chart does not roll "+
				"the DaemonSet on a config-only change (#669)",
			ciliumAgentRestartMinVersion,
		),
	}
}

// Execute restarts the Cilium agents when Kubernetes and Cilium are installed and
// acceleration is configured as "disabled" but may not yet be applied to the
// running agents. It no-ops on undeployed/unreachable hosts and when acceleration
// is not (yet) disabled — the acceleration migration owns flipping the config.
func (m *CiliumAgentRestartMigration) Execute(ctx context.Context, mctx *migration.Context) error {
	if !kubernetesInstalled() {
		logx.As().Debug().Msg("cilium agent restart migration: Kubernetes not installed on this host; skipping")
		return nil
	}

	installed, acc, err := readCiliumState(ctx)
	if err != nil {
		logx.As().Debug().Err(err).
			Msg("cilium agent restart migration: Kubernetes API not reachable; skipping")
		return nil
	}
	if !installed {
		logx.As().Debug().Msg("cilium agent restart migration: Cilium not installed (no cilium-config); skipping")
		return nil
	}
	if acc != disabledAcceleration {
		logx.As().Debug().Str("acceleration", acc).
			Msg("cilium agent restart migration: acceleration not 'disabled'; nothing to apply, skipping")
		return nil
	}

	logx.As().Info().Msg("Restarting Cilium agents to apply disabled acceleration (detach XDP)")
	if err := restartCiliumAgents(ctx); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to restart Cilium agents to apply disabled acceleration")
	}

	return nil
}

// Rollback is a no-op: restarting the agents is not something to undo.
func (m *CiliumAgentRestartMigration) Rollback(ctx context.Context, mctx *migration.Context) error {
	return nil
}

// defaultRestartCiliumAgents rolls the Cilium DaemonSet and waits for the rollout
// to finish, so the agents re-read bpf-lb-acceleration and detach XDP from the
// NICs.
func defaultRestartCiliumAgents(ctx context.Context) error {
	kc, err := kube.NewClient()
	if err != nil {
		return err
	}
	if err := kc.RolloutRestart(ctx, kube.KindDaemonSet, ciliumNamespace, ciliumDaemonSet); err != nil {
		return err
	}
	return kc.WaitForResource(ctx, kube.KindDaemonSet, ciliumNamespace, ciliumDaemonSet, daemonSetRolledOut, ciliumRolloutTimeout)
}

// daemonSetRolledOut is a kube.CheckFunc that reports true once a DaemonSet's
// restart has fully rolled out: the controller has observed the latest spec and
// every scheduled pod is updated and ready. Transient get errors keep it polling
// (detaching XDP can briefly blip the NIC the API server rides on).
func daemonSetRolledOut(obj *unstructured.Unstructured, err error) (bool, error) {
	if err != nil || obj == nil {
		return false, nil
	}
	generation, _, _ := unstructured.NestedInt64(obj.Object, "metadata", "generation")
	observed, _, _ := unstructured.NestedInt64(obj.Object, "status", "observedGeneration")
	desired, _, _ := unstructured.NestedInt64(obj.Object, "status", "desiredNumberScheduled")
	updated, _, _ := unstructured.NestedInt64(obj.Object, "status", "updatedNumberScheduled")
	ready, _, _ := unstructured.NestedInt64(obj.Object, "status", "numberReady")
	return observed >= generation && desired > 0 && updated == desired && ready == desired, nil
}
