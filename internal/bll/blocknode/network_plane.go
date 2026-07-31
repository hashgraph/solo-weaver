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
// healthPort is the resolved block-node health/statusz port (from
// blocknode.ResolveHealthPort against the operator's effective values), threaded
// into NetworkPolicyCreate so the bn-mgmt set tracks the port the BN actually
// listens on — matching the install path.
func networkPlaneSteps(ins models.BlockNodeInputs, force, trafficShapingEnabled, allowTeardown bool, healthPort string) []automa.Builder {
	var out []automa.Builder

	// Host firewall: desired-state convergence. NetworkFirewallCreate self-gates
	// on config.Host.Disabled, so it is always safe to include; a delete only runs
	// when teardown is permitted (reconfigure) AND the operator resolved the
	// firewall to disabled.
	if allowTeardown && config.Get().Host.Disabled {
		out = append(out, steps.NetworkFirewallDelete())
	} else {
		// reconcile=allowTeardown: reconfigure (allowTeardown=true) force
		// re-renders the host firewall so changed settings actually take effect;
		// upgrade (allowTeardown=false) re-asserts create-if-missing only.
		out = append(out, steps.NetworkFirewallCreate(allowTeardown))
	}

	switch {
	case trafficShapingEnabled:
		// Enable: create the BN policy plane, persist the weaver table, (re)provision
		// tc egress/ingress shaping, enable the daemon's traffic-shaper monitor, and
		// restart the daemon so that change takes effect. The daemon reads daemon.yaml
		// only at startup (no hot-reload) and the post-workflow ensureBlockNodeDaemon
		// is a no-op when the daemon is already running — so re-enabling on a host
		// where the daemon is up (e.g. a prior disable left it running with the
		// monitor off) would otherwise never start the monitor, and the ingress $VETH
		// HTB would never be (re)installed. Restarting here reloads daemon.yaml; the
		// monitor's initial pod list then shapes the current pod's veth immediately,
		// and the subsequent rollout-restart's new veth via a watch event.
		// RestartDaemonServiceStep self-skips when the daemon is not running (fresh
		// enable), where post-workflow ensureBlockNodeDaemon installs and starts it.
		shapeOverrides := toClassOverrides(ins.ShapeOverrides)
		out = append(out,
			steps.NetworkPolicyCreate(force, healthPort),
			steps.NftWeaverPersist(),
			steps.TcEgressPersist(ins.EgressInterface, ins.LinkRate, shapeOverrides),
			steps.TcIngressRecord(ins.EgressInterface, ins.LinkRate, shapeOverrides),
			steps.WriteBlockNodeDaemonConfigStep(models.Paths(), ins.Namespace, true, ins.StatuszBaseURL, ins.StatuszPollInterval),
			steps.RestartDaemonServiceStep(),
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
			// Disable path: no statusz override — turning the monitor off leaves any
			// operator-set statusz block on disk untouched.
			steps.WriteBlockNodeDaemonConfigStep(models.Paths(), ins.Namespace, false, "", ""),
			steps.RestartDaemonServiceStep(),
		)
	}

	return out
}
