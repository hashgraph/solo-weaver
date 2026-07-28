// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// networkPlaneSteps builds the static network-plane step list shared by
// `block node reconfigure` and `block node upgrade`. Both converge the host
// firewall and the BN traffic-shaping bundle (policy plane + tc HTB shaping +
// daemon traffic-shaper monitor) to a desired target, differing only in how that
// target is resolved and whether teardown is permitted:
//
//   - reconfigure resolves the target from the operator (prompt/flags) and passes
//     allowTeardown=true, so turning a feature off deletes it.
//   - upgrade resolves the target from persisted state and passes
//     allowTeardown=false: it re-asserts (create-if-missing) whatever the install
//     decision was, but never tears anything down from a routine version bump.
//
// Every step is idempotent, so the same list is safe to re-run: create steps are
// create-if-missing and teardown steps are delete-if-present. The step ordering
// mirrors NetworkSetupWorkflow — firewall first, then NetworkPolicyCreate before
// NftWeaverPersist so the policy registry is populated when the weaver table is
// rendered.
//
// trafficShapingEnabled is the resolved traffic-shaping target. force is the
// operator's --force, threaded into NetworkPolicyCreate for the enable path.
func networkPlaneSteps(ins models.BlockNodeInputs, force, trafficShapingEnabled, allowTeardown bool) []automa.Builder {
	var out []automa.Builder

	// Host firewall: desired-state convergence. NetworkFirewallCreate self-gates
	// on config.Host.Disabled, so it is always safe to include; a delete only runs
	// when teardown is permitted (reconfigure) AND the operator resolved the
	// firewall to disabled.
	if allowTeardown && config.Get().Host.Disabled {
		out = append(out, steps.NetworkFirewallDelete())
	} else {
		out = append(out, steps.NetworkFirewallCreate())
	}

	switch {
	case trafficShapingEnabled:
		// Enable: create the BN policy plane, persist the weaver table, (re)provision
		// tc egress/ingress shaping, and enable the daemon's traffic-shaper monitor.
		// Daemon binary/service activation itself is handled post-workflow in the CLI
		// (ensureBlockNodeDaemon), mirroring install.
		shapeOverrides := toClassOverrides(ins.ShapeOverrides)
		out = append(out,
			steps.NetworkPolicyCreate(force),
			steps.NftWeaverPersist(),
			steps.TcEgressPersist(ins.EgressInterface, ins.LinkRate, shapeOverrides),
			steps.TcIngressRecord(ins.EgressInterface, ins.LinkRate, shapeOverrides),
			steps.WriteBlockNodeDaemonConfigStep(models.Paths(), ins.Namespace, true),
		)
	case allowTeardown:
		// Disable: remove every BN policy (the last delete tears the inet weaver
		// table down, so no NftWeaverPersist is needed), drop the tc egress hierarchy,
		// turn the daemon's block-node traffic-shaper monitor off, and restart the
		// daemon so that change takes effect. The daemon reads daemon.yaml only at
		// startup (no hot-reload), so without the restart the live monitor keeps
		// running and re-applies the ingress $VETH HTB on the next BN pod create —
		// which, on the default reconfigure path, is triggered by the rollout-restart
		// that runs right after these network steps. Restarting here (before that pod
		// churn) ensures the monitor is gone when the new veth appears, so it comes up
		// unshaped. RestartDaemonServiceStep self-skips when the daemon is not running.
		out = append(out,
			steps.NetworkPolicyDeleteAll(),
			steps.TcEgressTeardown(),
			steps.WriteBlockNodeDaemonConfigStep(models.Paths(), ins.Namespace, false),
			steps.RestartDaemonServiceStep(),
		)
	}

	return out
}
