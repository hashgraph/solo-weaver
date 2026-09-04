// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/daemon"
	"github.com/hashgraph/solo-weaver/internal/network/shape"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// NetworkPlaneOptions parameterizes NetworkPlaneSteps for the three block-node
// commands that converge the static network plane (install, reconfigure,
// upgrade). All three share the same host-firewall + traffic-shaping step
// bundle; they differ only in the fields below (issue #947).
type NetworkPlaneOptions struct {
	// Force is the operator's --force, threaded into NetworkPolicyCreate.
	Force bool
	// TrafficShapingEnabled is the resolved traffic-shaping target: the BN
	// workload policy plane + tc HTB shaping + daemon monitor as one bundle.
	TrafficShapingEnabled bool
	// HealthPort is the resolved block-node health/statusz port, threaded into
	// NetworkPolicyCreate so the bn-health drop tracks the port the BN listens on.
	HealthPort string
	// Namespace is the block-node namespace, used by the daemon-config step when
	// WithDaemonReload is set.
	Namespace string
	// Statusz carries the operator-supplied statusz overrides (--statusz-base-url /
	// --statusz-poll-interval), threaded into the enable-path daemon-config write
	// when WithDaemonReload is set. The step merges it per-field into daemon.yaml;
	// an empty field leaves the existing on-disk value untouched. The disable path
	// deliberately writes a zero-value statusz so turning the monitor off never
	// touches an operator-set block.
	Statusz daemon.StatuszConfig
	// EgressInterface / LinkRate / ShapeOverrides feed the tc egress/ingress steps.
	EgressInterface string
	LinkRate        string
	ShapeOverrides  map[string]shape.ClassOverride

	// AllowTeardown permits deleting a feature resolved to off. reconfigure sets
	// it (turning a feature off deletes it); install/upgrade leave it false and
	// re-assert create-if-missing only, so a routine run never tears anything down.
	AllowTeardown bool
	// WithDaemonReload appends the daemon-config write + service restart inline
	// after the shaping steps so a live daemon reloads the changed daemon.yaml
	// (it reads the file only at startup — no hot-reload). reconfigure/upgrade set
	// this; install leaves it false and does the same write + restart later, in
	// BlockNodeDaemonConfigWorkflow, once the deployment the daemon watches
	// exists.
	WithDaemonReload bool
}

// NetworkPlaneSteps builds the static network-plane step list — host firewall
// (inet weaver-host-firewall) convergence plus, when enabled, the BN traffic-shaping bundle
// (policy plane + weaver table + $EGRESS/$VETH tc HTB shaping + daemon monitor)
// — shared by `block node install`, `reconfigure`, and `upgrade`. Every step is
// idempotent, so the same list is safe to re-run: create steps are
// create-if-missing and teardown steps are delete-if-present.
//
// The three commands drive it through NetworkPlaneOptions:
//   - install: AllowTeardown=false, WithDaemonReload=false (wrapped in the
//     "Network Setup" phase by NetworkSetupWorkflow; daemon handled separately).
//   - reconfigure: AllowTeardown=true, WithDaemonReload=true (operator-resolved
//     target, allowed to tear a feature down).
//   - upgrade: AllowTeardown=false, WithDaemonReload=true (persisted-state target,
//     re-assert create-if-missing only).
//
// Ordering: firewall first, then NetworkPolicyCreate before NftWeaverPersist so
// the policy registry is populated when the weaver table is rendered (an empty
// registry would persist a policy-drop chain).
func NetworkPlaneSteps(opts NetworkPlaneOptions) []automa.Builder {
	var out []automa.Builder

	// Host firewall: desired-state convergence. NetworkFirewallCreate self-gates
	// on config.Host.Disabled, so it is always safe to include; a delete only runs
	// when teardown is permitted (reconfigure) AND the operator resolved the
	// firewall to disabled. reconcile=AllowTeardown: reconfigure force re-renders
	// the host firewall so changed settings take effect; install/upgrade re-assert
	// create-if-missing only.
	if opts.AllowTeardown && config.Get().Host.Disabled {
		out = append(out, steps.NetworkFirewallDelete())
	} else {
		out = append(out, steps.NetworkFirewallCreate(opts.AllowTeardown))
	}

	switch {
	case opts.TrafficShapingEnabled:
		// Enable: create the BN policy plane, persist the weaver table, (re)provision
		// tc egress/ingress shaping, then — for the reload callers — enable the
		// daemon's traffic-shaper monitor and restart the daemon so the change takes
		// effect. The daemon reads daemon.yaml only at startup (no hot-reload) and the
		// post-workflow ensureBlockNodeDaemon is a no-op when the daemon is already
		// running, so re-enabling on a host where the daemon is up would otherwise
		// never start the monitor and the ingress $VETH HTB would never be
		// (re)installed. RestartDaemonServiceStep self-skips when the daemon is not
		// running (fresh enable), where post-workflow ensureBlockNodeDaemon installs it.
		out = append(out,
			steps.NetworkPolicyCreate(opts.Force, opts.HealthPort),
			steps.NftWeaverPersist(),
			steps.TcEgressPersist(opts.EgressInterface, opts.LinkRate, opts.ShapeOverrides),
			steps.TcIngressRecord(opts.EgressInterface, opts.LinkRate, opts.ShapeOverrides),
		)
		if opts.WithDaemonReload {
			out = append(out,
				steps.WriteBlockNodeDaemonConfigStep(models.Paths(), opts.Namespace, opts.Statusz, true),
				steps.RestartDaemonServiceStep(),
			)
		}
	case opts.AllowTeardown:
		// Disable (reconfigure only): remove every BN policy (the last delete tears
		// the inet weaver-workload-policy table down, so no NftWeaverPersist is needed), drop the tc
		// egress hierarchy, then turn the daemon's monitor off and restart the daemon.
		// The restart matters because the daemon reads daemon.yaml only at startup:
		// without it the live monitor keeps re-applying the ingress $VETH HTB on the
		// next BN pod create (triggered by the rollout-restart that runs right after
		// these steps). Restarting here — before that pod churn — ensures the monitor
		// is gone when the new veth appears, so it comes up unshaped.
		// RestartDaemonServiceStep self-skips when the daemon is not running.
		out = append(out,
			steps.NetworkPolicyDeleteAll(),
			steps.TcEgressDisable(),
		)
		if opts.WithDaemonReload {
			out = append(out,
				// Disable path carries no statusz override: turning the monitor off must
				// never touch an operator-set statusz block, and the step's per-field
				// merge leaves any on-disk block intact for a zero-value statusz.
				steps.WriteBlockNodeDaemonConfigStep(models.Paths(), opts.Namespace, daemon.StatuszConfig{}, false),
				steps.RestartDaemonServiceStep(),
			)
		}
	}

	return out
}

// NetworkSetupWorkflow lays down the block node's static network plane: the
// node-level host firewall (inet weaver-host-firewall), the weaver policy-table persistence
// (inet weaver-workload-policy), and the $EGRESS / $VETH tc HTB shape config. It is rendered as
// the "Network Setup" phase so these steps group under their own header in the
// TUI instead of dangling as loose sub-steps after the "Kubernetes Setup" phase.
//
// Ordering note preserved from the caller: this must run after the Kubernetes
// setup, since nftables is installed/enabled by the system-setup phase before the
// host firewall is applied here. NetworkPolicyCreate runs before NftWeaverPersist
// so the policy registry is populated when the weaver table is rendered and
// persisted; an empty registry would persist a policy-drop chain.
//
// trafficShapingEnabled gates the BN workload policy plane and tc shaping
// (NetworkPolicyCreate, NftWeaverPersist, TcEgressPersist, TcIngressRecord) as
// one bundle, independent of the host firewall (NetworkFirewallCreate, always
// included — it is gated separately by hostCfg.Disabled inside its own step).
// When false, none of the four steps are added: there is no inet weaver-workload-policy table
// to persist and no tc config to shape traffic with.
func NetworkSetupWorkflow(egressInterface, linkRate string, shapeOverrides map[string]shape.ClassOverride, force bool, trafficShapingEnabled bool, healthPort string) *automa.WorkflowBuilder {
	// Install shares the network-plane bundle with reconfigure/upgrade via
	// NetworkPlaneSteps but never tears down (AllowTeardown=false) and defers
	// the daemon config write + restart to BlockNodeDaemonConfigWorkflow, after
	// the deployment it watches is in place (WithDaemonReload=false).
	stepList := NetworkPlaneSteps(NetworkPlaneOptions{
		Force:                 force,
		TrafficShapingEnabled: trafficShapingEnabled,
		HealthPort:            healthPort,
		EgressInterface:       egressInterface,
		LinkRate:              linkRate,
		ShapeOverrides:        shapeOverrides,
		AllowTeardown:         false,
		WithDaemonReload:      false,
	})

	return automa.NewWorkflowBuilder().
		WithId("block-node-network-setup").
		Steps(stepList...).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().PhaseStart(ctx, stp, "Network Setup")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().PhaseFailure(ctx, stp, rpt, "Network Setup")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().PhaseCompletion(ctx, stp, rpt, "Network Setup")
		})
}

// BlockNodeDaemonConfigWorkflow enables the block-node traffic-shaper monitor
// in daemon.yaml and restarts the daemon so it actually loads it. It renders as
// the "Traffic-shaper Monitor" phase and runs last, once the deployment it
// watches is in place.
//
// The restart is what fixes #1018: the daemon reads daemon.yaml only at
// startup, and uninstall leaves the shared unit running — so without it, a
// reinstall would leave the daemon on its stale config and the monitor would
// never start. On a fresh host both extra steps skip (no daemon yet) and
// ensureBlockNodeDaemon installs it post-workflow, as before. The final probe
// fails the phase if the restarted daemon could not build the block-node
// component (e.g. its kubeconfig no longer reaches the cluster).
//
// statusz carries the operator-supplied overrides (--statusz-base-url /
// --statusz-poll-interval), merged per-field into daemon.yaml by the step; an
// empty field leaves the existing on-disk statusz untouched.
func BlockNodeDaemonConfigWorkflow(namespace string, statusz daemon.StatuszConfig) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().
		WithId("block-node-daemon-config").
		Steps(
			steps.WriteBlockNodeDaemonConfigStep(models.Paths(), namespace, statusz, true),
			steps.RestartDaemonServiceStep(),
			steps.CheckDaemonComponentPrerequisitesStep(models.Paths().DaemonSockPath, daemon.ComponentNameBlockNode),
		).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().PhaseStart(ctx, stp, "Traffic-shaper Monitor")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().PhaseFailure(ctx, stp, rpt, "Traffic-shaper Monitor")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().PhaseCompletion(ctx, stp, rpt, "Traffic-shaper Monitor")
		})
}
