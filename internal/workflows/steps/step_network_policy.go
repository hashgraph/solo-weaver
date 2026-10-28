// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/network/policy"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
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
	// healthPort marks a policy whose ports come from the resolved block-node
	// health port (chart blockNode.ports.health) rather than a literal or from
	// statusz. It bootstraps statusz discovery (you fetch statusz on it), so it
	// cannot itself be read back from statusz.
	healthPort bool
	// publisherPort marks a policy whose ports come from the resolved block-node
	// publish port (chart blockNode.ports.publisher, else service.port). It is
	// pinned rather than read from statusz because statusz only reports it for
	// configured publishers: with an empty roster the port set would stay empty
	// and the deny-non-members guard would match nothing.
	publisherPort bool
	// denyNonMembers drops sources outside the policy's membership set on its
	// ports. It has no `network policy create` flag: only the canonical set uses it.
	denyNonMembers bool
}

// bnPorts carries the block-node facility ports pinned from the chart values.
type bnPorts struct {
	health    string
	publisher string
}

// canonicalBNPolicies is the fixed set of policies `block node install` creates,
// in creation order. The daemon binds the classification sets to statusz
// categories at runtime; these definitions are statusz-agnostic. --stamp
// references the class names in the stable mark map; each class fixes its own
// direction, so there is no direction flag.
//
// The subscriber, block-access and server-status listener ports are read back
// from the BN's statusz local.port and reconciled into the per-policy
// `<name>_ports` sets by the daemon (managedPorts). Server-status is public to
// everyone, so it rides the public port union on bn-subscriber-in (ingress) and
// bn-public-out (egress) rather than a dedicated set (see obsoleteBNPolicies).
// The health and publish ports are pinned from the chart values instead
// (healthPort, publisherPort), because each must be known before statusz can
// report it.
//
// bn-publisher drops every source outside its membership set on the publish
// port, so only publishers the BN lists in known-publishers.json can stream
// blocks. On a block node that serves every facility on one port, that
// restricts the subscriber, block-access and server-status APIs to the same
// sources.
//
// bn-health drops the health/statusz port from every source. Its only legitimate
// consumers are the node's kubelet and the provisioner, and both generate their
// traffic on the node itself — so those packets take output/postrouting and
// never reach the forward hook this table registers on. Anything that does reach
// it is off-node by construction and has no business on that port.
var canonicalBNPolicies = []canonicalPolicy{
	{name: "bn-publisher", publisherPort: true, stamp: "publisher", denyNonMembers: true},
	{name: "bn-subscriber-in", managedPorts: true, stamp: "reserve-ingress", fromWorld: true},
	{name: "bn-partner-out", managedPorts: true, stamp: "partner"},
	{name: "bn-public-out", managedPorts: true, stamp: "public", fromWorld: true},
	{name: "bn-health", healthPort: true, deny: true, fromWorld: true},
	{name: "bn-restricted", deny: true},
	{name: "bn-backfill", stamp: "reserve-egress", replyStamp: "backfill-response"},
}

// obsoleteBNPolicies are policies a prior solo-weaver release created that this
// release no longer owns. On upgrade they still sit in the policy registry (and
// the live table, which Manager re-renders from the registry), so the install
// step deletes them explicitly rather than leaving orphaned sets behind.
// bn-status-in / bn-status-out were folded into the public port union on
// bn-subscriber-in / bn-public-out (server-status is public to everyone).
// bn-mgmt-in / bn-mgmt-out modelled the health port as reachable from an
// operator-supplied management allowlist; bn-health replaces them by dropping it
// from every off-node source instead.
var obsoleteBNPolicies = []string{"bn-status-in", "bn-status-out", "bn-mgmt-in", "bn-mgmt-out"}

// toPolicy builds the policy.Policy for a canonical entry, resolving a
// healthPort or publisherPort entry's ports from ports. Action/Direction are
// resolved from the stamp/deny fields; Validate (called inside Manager.Create)
// derives Direction from the class and rejects any invalid combination.
func (c canonicalPolicy) toPolicy(ports bnPorts) *policy.Policy {
	listen := c.ports
	switch {
	case c.healthPort:
		listen = []string{ports.health}
	case c.publisherPort:
		listen = []string{ports.publisher}
	}
	p := &policy.Policy{
		Name:            c.name,
		Stamp:           c.stamp,
		ReplyStamp:      c.replyStamp,
		Ports:           listen,
		ManagedPorts:    c.managedPorts,
		FromEntityWorld: c.fromWorld,
		DenyNonMembers:  c.denyNonMembers,
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
// by the manager). No membership is seeded here: every set starts empty and is
// filled by the daemon's statusz poll loop.
// healthPort and publisherPort are the resolved block-node health/statusz and
// publish ports (blocknode.ResolveHealthPort / ResolvePublisherPort against the
// operator's effective values), so the ports bn-health and bn-publisher match
// track the ports the BN actually listens on.
//
// A canonical policy that already exists with a different definition — one a
// prior release created — is redefined in place with its live membership kept,
// so an upgrade converges without --force and without cutting off peers.
func NetworkPolicyCreate(force bool, healthPort, publisherPort string) *automa.StepBuilder {
	ports := bnPorts{health: healthPort, publisher: publisherPort}
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

				// Install-time wiring auto-detects a single (v4) pod CIDR; dual-stack
				// v6 classification is opt-in via the `network policy create --pod-cidr`
				// CLI (Manager.Create accepts a mixed v4/v6 list).
				changed, err := mgr.Create(ctx, c.toPolicy(ports), nil, []string{podCIDR}, force)
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
				if preExisting && !force {
					redefined, err := mgr.Redefine(ctx, c.toPolicy(ports), []string{podCIDR})
					if err != nil {
						stp.State().Local().Set(policyCreatedNamesKey, created)
						return automa.FailureReport(stp, automa.WithError(errx.Decorate(
							errorx.Decorate(err, "failed to update network policy %q to this release's definition", c.name),
							reasons.PreconditionNotMet,
							"Inspect the policy registry entry: cat "+policy.RegistryDir+"/"+c.name+".json",
							"Check the rendered chain for syntax errors: nft -c -f "+policy.WeaverNftPath,
							"Re-run with --force to replace the policy outright (its membership is repopulated by the daemon)",
						)))
					}
					if redefined {
						logx.As().Info().Str("policy", c.name).Msg("updated network policy to this release's definition, keeping its live membership")
					}
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
					Msg("removed obsolete network policy")
			}

			stp.State().Local().Set(policyCreatedNamesKey, created)
			logx.As().Info().Int("created", len(created)).Int("total", len(canonicalBNPolicies)).
				Msg("network policies reconciled")
			warnIfNoPublishers(ctx, stp, mgr, publisherPort)
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

// knownPublishersPath is the block node's default publisher roster, inside the
// BN pod. Its entries reach @bn-publisher through statusz/inbound.
const knownPublishersPath = "/opt/hiero/block-node/application-state/known-publishers.json"

// warnIfNoPublishers tells the operator when bn-publisher has no members: its
// deny-non-members guard then closes the publish port to every source. On a
// fresh install that is expected until the daemon's first statusz poll, so it is
// a warning, not a failure. A failed read is logged and otherwise ignored — it
// says nothing about whether the roster is configured.
func warnIfNoPublishers(ctx context.Context, stp automa.Step, mgr *policy.Manager, publisherPort string) {
	members, err := mgr.Members(ctx, "bn-publisher")
	if err != nil {
		logx.As().Warn().Err(err).Msg("could not read bn-publisher membership; skipping the empty-roster check")
		return
	}
	if len(members) > 0 {
		return
	}
	logx.As().Warn().Str("port", publisherPort).Str("roster", knownPublishersPath).
		Msg("bn-publisher has no members: the publish port is closed to every source until the block node reports a known publisher")
	notify.As().StepDetail(ctx, stp,
		"Publish port %s accepts only known publishers, and none are configured yet. Add them to %s in the block node; the daemon admits them on its next statusz poll. Check with: solo-provisioner network policy show --name bn-publisher",
		publisherPort, knownPublishersPath)
}
