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
var canonicalBNPolicies = []canonicalPolicy{
	{name: "bn-publisher", ports: []string{"40840"}, stamp: "publisher"},
	{name: "bn-subscriber-in", ports: []string{"40980", "40981"}, stamp: "reserve-ingress", fromWorld: true},
	{name: "bn-partner-out", ports: []string{"40980", "40981"}, stamp: "partner"},
	{name: "bn-public-out", ports: []string{"40980", "40981"}, stamp: "public", fromWorld: true},
	{name: "bn-status-in", ports: []string{"40982"}, stamp: "reserve-ingress", fromWorld: true},
	{name: "bn-status-out", ports: []string{"40982"}, stamp: "public", fromWorld: true},
	{name: "bn-mgmt-in", ports: []string{"40983"}, stamp: "reserve-ingress", curated: true},
	{name: "bn-mgmt-out", ports: []string{"40983"}, stamp: "reserve-egress", curated: true},
	{name: "bn-restricted", deny: true},
	{name: "bn-backfill", stamp: "reserve-egress", replyStamp: "backfill-response"},
}

// toPolicy builds the policy.Policy for a canonical entry. Action/Direction are
// resolved from the stamp/deny fields; Validate (called inside Manager.Create)
// derives Direction from the class and rejects any invalid combination.
func (c canonicalPolicy) toPolicy() *policy.Policy {
	p := &policy.Policy{
		Name:            c.name,
		Stamp:           c.stamp,
		ReplyStamp:      c.replyStamp,
		Ports:           c.ports,
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
// network-weaver.nft (an empty registry would render a policy-drop chain).
//
// Every create is idempotent: a re-run leaves existing policies and their
// operator-mutated set membership untouched. When force is set, each policy's
// static rules are re-rendered from these definitions (membership is preserved
// by the manager). The one operator-curated set, bn-mgmt-in/out, receives its
// initial membership here from the host management allowlist (--mgmt-cidrs).
// bn-restricted starts empty and is left entirely to the daemon's statusz poll
// loop — see canonicalPolicy.curated.
func NetworkPolicyCreate(force bool) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(NetworkPolicyCreateStepId).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Creating network policies (inet weaver)")
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
				changed, err := mgr.Create(ctx, c.toPolicy(), cidrs, podCIDR, force)
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
								"Inspect the live weaver table: nft list table inet weaver",
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
