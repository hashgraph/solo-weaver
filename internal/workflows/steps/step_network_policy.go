// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/network/policy"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// NetworkPolicyCreateStepId is the step ID for NetworkPolicyCreate.
const NetworkPolicyCreateStepId = "network-policy-create"

// policyCreatedNamesKey is the step-local state key under which the names of the
// policies this step actually created are recorded, so rollback removes only
// those (never a policy that pre-existed the install).
const policyCreatedNamesKey = "networkPolicyCreatedNames"

// newPolicyManager is the seam over the production policy manager so unit tests
// can substitute a manager wired to a fake nft runner and temp paths (the
// production manager restarts a systemd service, which is Linux-only).
var newPolicyManager = func() *policy.Manager { return policy.NewManager() }

// detectPolicyPodCIDR resolves the local node's pod CIDR from the cluster. It is
// indirected through a var so unit tests can stub cluster access.
var detectPolicyPodCIDR = func(ctx context.Context) (string, error) {
	c, err := kube.NewClient()
	if err != nil {
		return "", err
	}
	return c.DetectNodePodCIDR(ctx)
}

// canonicalPolicy describes one entry in the fixed BN static-plane policy set
// laid down at install. Its fields mirror the `network policy create` flags: the
// install is a thin orchestrator that runs the equivalent of one create per
// category. The vocabulary (names, classes, ports) is fixed by the BN's
// contract, not operator-configurable — only the curated sets' initial
// membership is supplied by the operator.
type canonicalPolicy struct {
	name       string
	stamp      string // --stamp class ("" for a deny policy)
	replyStamp string // --reply-stamp class (asymmetric conntrack reply)
	deny       bool   // --deny (drop both directions)
	fromWorld  bool   // --from-entity world (no IP-set clause)
	ports      []string
	// managedPorts marks a policy whose `<name>_ports` listener-port set is
	// reconciled by the traffic-shaper daemon from the BN's statusz local.port,
	// not seeded here. Such a set is rendered empty at install and filled on the
	// first poll tick (the same empty-and-fill contract the CIDR membership sets
	// have), so no port literal is baked into the inet weaver-workload-policy chain.
	managedPorts bool
	// healthPort marks the bn-mgmt sets, whose ports come from the resolved
	// block-node health port (chart blockNode.ports.health) rather than a literal
	// or from statusz. The health port is the one a-priori facility port: it
	// bootstraps statusz discovery (you fetch statusz on it), so it cannot itself
	// be read back from statusz.
	healthPort bool
	// curated marks an operator-curated set (bn-mgmt-*) that receives its
	// initial membership from operator input at create time rather than from
	// the daemon's statusz poll loop. bn-restricted is NOT curated: it reflects
	// a "restricted" category the block node itself reports via statusz, fully
	// reconciled by the daemon every poll tick — an operator-supplied seed
	// would just be overwritten on the first tick. A permanent, purely
	// operator-managed block list lives instead on the host firewall
	// (`network firewall --blocked-cidrs`, a different table entirely).
	curated bool
}

// canonicalBNPolicies is the fixed set of policies `block node install` creates,
// in creation order. The daemon binds the classification sets to statusz
// categories at runtime; these definitions are statusz-agnostic. --stamp
// references the class names in the stable mark map; each class fixes its own
// direction, so there is no direction flag.
//
// Listener ports are no longer baked in as literals: the publisher, subscriber,
// block-access and server-status ports are read back from the BN's statusz
// local.port and reconciled into the per-policy `<name>_ports` sets by the
// daemon (managedPorts). Server-status is public to everyone, so it rides the
// public port union on bn-subscriber-in (ingress) and bn-public-out (egress)
// rather than a dedicated set — the old bn-status-in/bn-status-out policies are
// folded away (see obsoleteBNPolicies). Only the bn-mgmt health port is pinned
// here, from the chart's blockNode.ports.health (healthPort), because it
// bootstraps statusz discovery and cannot come from statusz itself.
var canonicalBNPolicies = []canonicalPolicy{
	{name: "bn-publisher", managedPorts: true, stamp: "publisher"},
	{name: "bn-subscriber-in", managedPorts: true, stamp: "reserve-ingress", fromWorld: true},
	{name: "bn-partner-out", managedPorts: true, stamp: "partner"},
	{name: "bn-public-out", managedPorts: true, stamp: "public", fromWorld: true},
	{name: "bn-mgmt-in", healthPort: true, stamp: "reserve-ingress", curated: true},
	{name: "bn-mgmt-out", healthPort: true, stamp: "reserve-egress", curated: true},
	{name: "bn-restricted", deny: true},
	{name: "bn-backfill", stamp: "reserve-egress", replyStamp: "backfill-response"},
}

// obsoleteBNPolicies are policies a prior solo-weaver release created that this
// release no longer owns. On upgrade they still sit in the policy registry (and
// the live table, which Manager re-renders from the registry), so the install
// step deletes them explicitly rather than leaving orphaned sets behind.
// bn-status-in / bn-status-out were folded into the public port union on
// bn-subscriber-in / bn-public-out (server-status is public to everyone).
var obsoleteBNPolicies = []string{"bn-status-in", "bn-status-out"}

// toPolicy builds the policy.Policy for a canonical entry, resolving a
// healthPort entry's ports to the given block-node health port. Action/Direction
// are resolved from the stamp/deny fields; Validate (called inside
// Manager.Create) derives Direction from the class and rejects any invalid
// combination.
func (c canonicalPolicy) toPolicy(healthPort string) *policy.Policy {
	ports := c.ports
	if c.healthPort {
		ports = []string{healthPort}
	}
	p := &policy.Policy{
		Name:            c.name,
		Stamp:           c.stamp,
		ReplyStamp:      c.replyStamp,
		Ports:           ports,
		ManagedPorts:    c.managedPorts,
		FromEntityWorld: c.fromWorld,
	}
	if c.deny {
		p.Action = policy.ActionDeny
	} else {
		p.Action = policy.ActionStamp
	}
	return p
}

// NetworkPolicyCreate lays down the BN workload classification plane (the `inet
// weaver` table) by running the create-if-missing equivalent of `network policy
// create` for each canonical BN category. It must run before NftWeaverPersist so
// the policy registry is populated when that step re-renders and persists
// network-weaver-workload-policy.nft (an empty registry persists no file at all).
//
// Every create is idempotent: a re-run leaves existing policies and their
// operator-mutated set membership untouched. When force is set, each policy's
// static rules are re-rendered from these definitions (membership is preserved
// by the manager). The one operator-curated set, bn-mgmt-in/out, receives its
// initial membership here from the host management allowlist (--mgmt-cidrs).
// bn-restricted starts empty and is left entirely to the daemon's statusz poll
// loop — see canonicalPolicy.curated.
// healthPort is the resolved block-node health/statusz port (from
// blocknode.ResolveHealthPort against the operator's effective values), used to
// seed the bn-mgmt sets so the port solo-weaver allows tracks the port the BN
// actually listens on rather than a value baked into solo-weaver.
func NetworkPolicyCreate(force bool, healthPort string) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(NetworkPolicyCreateStepId).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Creating network policies (inet weaver-workload-policy)")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to create network policies")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Network policies created")
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			mgmtCIDRs := config.Get().Host.ManagementCIDRs

			// The pod CIDR scopes every --stamp classification rule to the local
			// node's pods. It is auto-detected from the node's .spec.podCIDR at
			// install time (matching `network policy create`), not taken from the
			// host firewall's --pod-cidr — that flag carries the broader
			// cluster-wide subnet used for the in-cluster host-service rule, which
			// would over-match here. A deny-only plane would not need it, but every
			// BN plane has --stamp policies.
			podCIDR, err := detectPolicyPodCIDR(ctx)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.Decorate(err, "failed to auto-detect the pod CIDR for network policy classification").
						WithProperty(models.ErrPropertyResolution, []string{
							"Verify the cluster is reachable: kubectl get nodes",
							"Check the local node has a pod CIDR: kubectl get nodes -o jsonpath='{.items[*].spec.podCIDR}'",
							"Ensure kubeconfig is present for the provisioner (block node install runs against a live cluster)",
						})))
			}
			logx.As().Info().Str("pod_cidr", podCIDR).Msg("auto-detected pod CIDR for network policies")

			mgr := newPolicyManager()
			var created []string
			for _, c := range canonicalBNPolicies {
				// Record for rollback only policies that did not already exist:
				// Manager.Create also returns changed=true when it replaces a
				// pre-existing policy (--force) or self-heals a missing live table
				// under an existing registry entry, and rollback must never delete
				// an operator-owned policy this step did not create.
				preExisting, err := policy.Exists(c.name)
				if err != nil {
					stp.State().Local().Set(policyCreatedNamesKey, created)
					return automa.FailureReport(stp, automa.WithError(
						errorx.Decorate(err, "failed to check whether network policy %q already exists", c.name).
							WithProperty(models.ErrPropertyResolution, []string{
								"Inspect the policy registry: ls " + policy.RegistryDir,
							})))
				}

				cidrs := initialCIDRs(c, mgmtCIDRs)
				// Install-time wiring auto-detects a single (v4) pod CIDR; dual-stack
				// v6 classification is opt-in via the `network policy create --pod-cidr`
				// CLI (Manager.Create accepts a mixed v4/v6 list).
				changed, err := mgr.Create(ctx, c.toPolicy(healthPort), cidrs, []string{podCIDR}, force)
				if err != nil {
					stp.State().Local().Set(policyCreatedNamesKey, created)
					return automa.FailureReport(stp, automa.WithError(
						errorx.Decorate(err, "failed to create network policy %q", c.name).
							WithProperty(models.ErrPropertyResolution, []string{
								"Inspect the policy registry: ls " + policy.RegistryDir,
								"Check the rendered chain for syntax errors: nft -c -f " + policy.WeaverNftPath,
								"Re-run the install; policy creation is idempotent (create-if-missing)",
							})))
				}
				if changed && !preExisting {
					created = append(created, c.name)
				}
			}
			// Remove policies a prior release created that this release folded
			// away (bn-status-*). On upgrade they still sit in the registry, and
			// Manager re-renders the whole table from it, so leaving them would
			// keep orphaned sets alive. Idempotent: absent policies are skipped,
			// so a fresh install (where they never existed) is a no-op.
			for _, name := range obsoleteBNPolicies {
				exists, err := policy.Exists(name)
				if err != nil {
					stp.State().Local().Set(policyCreatedNamesKey, created)
					return automa.FailureReport(stp, automa.WithError(
						errorx.Decorate(err, "failed to check whether obsolete network policy %q exists", name).
							WithProperty(models.ErrPropertyResolution, []string{
								"Inspect the policy registry: ls " + policy.RegistryDir,
							})))
				}
				if !exists {
					continue
				}
				if err := mgr.Delete(ctx, name); err != nil {
					stp.State().Local().Set(policyCreatedNamesKey, created)
					return automa.FailureReport(stp, automa.WithError(
						errorx.Decorate(err, "failed to remove obsolete network policy %q", name).
							WithProperty(models.ErrPropertyResolution, []string{
								"Inspect the live weaver table: nft list table inet weaver-workload-policy",
								"Check the policy registry for leftover entries: ls " + policy.RegistryDir,
							})))
				}
				logx.As().Info().Str("policy", name).
					Msg("removed obsolete network policy (folded into the public port union)")
			}

			stp.State().Local().Set(policyCreatedNamesKey, created)
			logx.As().Info().Int("created", len(created)).Int("total", len(canonicalBNPolicies)).
				Msg("network policies reconciled")
			return automa.SuccessReport(stp)
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			// Remove only the policies this step created; a pre-existing policy
			// (create-if-missing found it already present, or a --force replace
			// of one the operator owns) is left intact. Deleting in reverse
			// creation order keeps the last delete — which tears the whole table
			// down — for the case where this step created every policy.
			created := automa.StringSliceFromState(stp.State().Local(), policyCreatedNamesKey)
			if len(created) == 0 {
				return automa.SkippedReport(stp,
					automa.WithDetail("no network policies were created by this step, skipping rollback"))
			}
			mgr := newPolicyManager()
			for i := len(created) - 1; i >= 0; i-- {
				if err := mgr.Delete(ctx, created[i]); err != nil {
					return automa.FailureReport(stp, automa.WithError(
						errorx.Decorate(err, "failed to roll back network policy %q", created[i]).
							WithProperty(models.ErrPropertyResolution, []string{
								"Inspect the live weaver table: nft list table inet weaver-workload-policy",
								"Complete teardown of the workload plane: block node uninstall",
								"Check the policy registry for leftover entries: ls " + policy.RegistryDir,
							})))
				}
			}
			return automa.SuccessReport(stp)
		})
}

// initialCIDRs returns the initial set membership supplied at create time for a
// canonical policy: the host management allowlist for the bn-mgmt-* sets, and
// nil for every daemon-reconciled set (whose membership arrives from the
// statusz poll loop instead), including bn-restricted.
func initialCIDRs(c canonicalPolicy, mgmtCIDRs []string) []string {
	if !c.curated {
		return nil
	}
	return mgmtCIDRs
}
