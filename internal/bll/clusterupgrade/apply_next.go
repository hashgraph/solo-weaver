// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"context"
	"reflect"
	"strings"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"

	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// Outcome is how an apply run ended.
type Outcome string

const (
	// OutcomeApplied: one hop was applied.
	OutcomeApplied Outcome = "applied"
	// OutcomeWalked: every remaining hop was applied.
	OutcomeWalked Outcome = "walked"
	// OutcomeAtTarget: there was nothing left to apply.
	OutcomeAtTarget Outcome = "at-target"
	// OutcomeBlocked: the plan has blockers; nothing was changed.
	OutcomeBlocked Outcome = "blocked"
	// OutcomeDeclined: the operator did not confirm; nothing was changed.
	OutcomeDeclined Outcome = "declined"
	// OutcomeStopped: the walk stopped after some hops because the next plan
	// could not be made. A re-run continues from where the cluster is.
	OutcomeStopped Outcome = "stopped"
	// OutcomeFailed: a hop failed.
	OutcomeFailed Outcome = "failed"
)

// ApplyResult is what an apply run did.
type ApplyResult struct {
	Outcome   Outcome   `json:"outcome"`
	Target    Target    `json:"target"`
	Applied   []Hop     `json:"applied"`
	Remaining []Hop     `json:"remaining"`
	Blockers  []Blocker `json:"blockers,omitempty"`
	// Cause says why the run stopped, failed or left a warning, for people.
	Cause string `json:"cause,omitempty"`
}

// ApplyOptions are the options of Apply.
type ApplyOptions struct {
	Options
	// All applies every remaining hop instead of only the next one.
	All bool
	// Confirm is asked, under the locks, with the hops the run will apply. A
	// nil Confirm proceeds.
	Confirm func(hops []Hop) (bool, error)
}

// Seams so tests need no kubeconfig and can script the plans and the hops.
var (
	newKubeClient = kube.NewClient
	livePlan      = PlanLive
	applyOneHop   = ApplyHop
)

// ApplyHop runs hop with the executor for its kind.
func ApplyHop(ctx context.Context, src LiveSources, locks *state.HeldLocks, run WorkflowRunner, hop Hop) error {
	switch hop.Kind {
	case KindCilium:
		return ApplyCiliumHop(ctx, src, locks, run, hop)
	case KindKubernetes:
		return ApplyKubernetesHop(ctx, src, locks, run, hop)
	case KindStructural:
		return ApplyStructuralHop(ctx, src, locks, run, hop)
	case KindCharts:
		return ApplyChartsHop(ctx, src, locks, run, hop)
	}
	return errx.Decorate(errorx.IllegalArgument.New("no executor for %q hops", hop.Kind), reasons.Internal)
}

// HopChecks runs the pre-hop checks of hop's kind on shape.
func HopChecks(shape rsl.ClusterShape, m Matrix, hop Hop) (Checks, error) {
	switch hop.Kind {
	case KindCilium:
		return CiliumHopChecks(shape, m, hop)
	case KindKubernetes:
		return KubernetesHopChecks(shape, m, hop)
	case KindStructural:
		return StructuralHopChecks(shape, m, hop)
	case KindCharts:
		return ChartsHopChecks(shape, m, hop)
	}
	return nil, errx.Decorate(errorx.IllegalArgument.New("no checks for %q hops", hop.Kind), reasons.Internal)
}

// DryRunResult is the next hop of the live plan and its pre-hop checks.
type DryRunResult struct {
	Plan   Plan   `json:"plan"`
	Next   *Hop   `json:"next,omitempty"`
	Checks Checks `json:"checks,omitempty"`
}

// DryRun plans from the live cluster and runs the next hop's pre-hop checks. It
// takes no lock and changes nothing. The checks that need the live cluster in
// detail run only when the hop is applied.
func DryRun(ctx context.Context, src LiveSources, opts Options) (DryRunResult, error) {
	shape, m, reach, err := readLive(ctx, src)
	if err != nil {
		return DryRunResult{}, err
	}
	var plan Plan
	if !reach.Observed() {
		plan, err = blockedPlan(m, opts, unreachableBlocker(reach))
	} else {
		plan, err = Resolve(shape, m, opts)
	}
	if err != nil || len(plan.Blockers) > 0 || len(plan.Hops) == 0 {
		return DryRunResult{Plan: plan}, err
	}
	next := plan.Hops[0]
	checks, err := HopChecks(shape, m, next)
	if err != nil {
		return DryRunResult{}, err
	}
	return DryRunResult{Plan: plan, Next: &next, Checks: checks}, nil
}

// Apply applies the next hop of the live plan, or with opts.All every remaining
// hop, re-planning from the live cluster after each one. The caller holds the
// system and cluster locks in locks for the whole run. When the cluster
// reaches or is at the target, it records the cluster and the host binaries,
// so a record an earlier run could not write is written now. The result is
// returned with the error too, with Cause set when the run did not finish.
func Apply(ctx context.Context, src LiveSources, locks *state.HeldLocks, run WorkflowRunner, opts ApplyOptions) (res ApplyResult, err error) {
	res = ApplyResult{Applied: []Hop{}, Remaining: []Hop{}}
	defer func() {
		if err != nil {
			if res.Outcome == "" {
				res.Outcome = OutcomeFailed
			}
			if res.Cause == "" {
				res.Cause = doctor.OperatorMessage(err)
			}
		}
	}()

	if run == nil {
		return res, errx.Decorate(errorx.IllegalArgument.New("a workflow runner must be set"), reasons.Internal)
	}
	if err := heldFor(src, locks, state.ComponentSystem, state.ComponentCluster); err != nil {
		return res, err
	}
	plan, err := livePlan(ctx, src, opts.Options)
	if err != nil {
		return res, err
	}
	res.Target = plan.Target
	if len(plan.Blockers) > 0 {
		res.Outcome, res.Blockers = OutcomeBlocked, plan.Blockers
		return res, blockedError(plan.Blockers)
	}
	res.Remaining = plan.Hops

	if ctx, err = withKubeClient(ctx); err != nil {
		return res, err
	}
	if len(plan.Hops) == 0 {
		logx.As().Info().Str("kubernetes", plan.Target.Kubernetes).Msg("The cluster is at the target; recording it")
		if err := recordAtTarget(ctx, src, run); err != nil {
			return res, err
		}
		res.Outcome = OutcomeAtTarget
		return res, nil
	}

	// With --all the walk runs only the hops the operator confirmed: a new plan
	// that is not what is left of them stops it.
	var confirmed []Hop
	if opts.Confirm != nil {
		confirmed = plan.Hops[:1]
		if opts.All {
			confirmed = plan.Hops
		}
		ok, err := opts.Confirm(confirmed)
		if err != nil {
			return res, errx.Decorate(errorx.InternalError.Wrap(err, "failed to ask for confirmation"), reasons.Internal)
		}
		if !ok {
			res.Outcome = OutcomeDeclined
			return res, errx.Decorate(errorx.RejectedOperation.New("the upgrade was not confirmed; nothing was changed"),
				reasons.PreconditionNotMet, "Run the command again and confirm, or pass --force to skip the question.")
		}
	}

	limit := len(plan.Hops) + 2
	for {
		hop := plan.Hops[0]
		if n := len(res.Applied); n > 0 && reflect.DeepEqual(res.Applied[n-1], hop) {
			res.Remaining = plan.Hops
			return res, errx.Decorate(errorx.IllegalState.New("the plan still starts with the %s hop just applied", hop.Describe()),
				reasons.Internal)
		}
		if len(res.Applied) >= limit {
			res.Remaining = plan.Hops
			return res, errx.Decorate(errorx.IllegalState.New("the upgrade did not reach its target in %d hops", limit),
				reasons.Internal)
		}

		logx.As().Info().Str("kind", string(hop.Kind)).Str("step", string(hop.Step)).Str("from", hop.From).Str("to", hop.To).
			Msg("Applying a cluster upgrade hop")
		if err := applyOneHop(ctx, src, locks, run, hop); err != nil {
			res.Remaining = plan.Hops
			logx.As().Warn().Err(err).Str("kind", string(hop.Kind)).Msg("The cluster upgrade hop failed")
			return res, err
		}
		res.Applied = append(res.Applied, hop)
		logx.As().Info().Str("kind", string(hop.Kind)).Str("to", hop.To).Msg("Applied a cluster upgrade hop")

		next, err := livePlan(ctx, src, opts.Options)
		if err != nil || len(next.Blockers) > 0 {
			cause := "the next plan could not be made"
			if err != nil {
				cause += ": " + doctor.OperatorMessage(err)
			} else {
				cause += ": " + blockerSummary(next.Blockers)
			}
			res.Remaining, res.Blockers, res.Cause = []Hop{}, next.Blockers, cause
			logx.As().Warn().Err(err).Msg("The hop was applied, but the next plan could not be made")
			if !opts.All {
				res.Outcome = OutcomeApplied
				return res, nil
			}
			res.Outcome = OutcomeStopped
			return res, errx.Decorate(errorx.IllegalState.New("the upgrade stopped after %d hop(s): %s", len(res.Applied), cause),
				reasons.PreconditionNotMet,
				"Run 'solo-provisioner kube cluster upgrade --plan --experimental' to see why, then run the upgrade again.")
		}
		res.Remaining = next.Hops

		if len(next.Hops) == 0 {
			res.Outcome = OutcomeWalked
			if !opts.All {
				res.Outcome = OutcomeApplied
			}
			// The last hop recorded the cluster; this also records the host
			// binaries that hops of other kinds do not.
			if err := recordAtTarget(ctx, src, run); err != nil {
				res.Cause = "the cluster is at the target, but recording it failed: " + doctor.OperatorMessage(err)
				logx.As().Warn().Err(err).Msg("The cluster is at the target, but recording it failed")
			}
			return res, nil
		}
		if !opts.All {
			res.Outcome = OutcomeApplied
			return res, nil
		}
		if confirmed != nil && !reflect.DeepEqual(next.Hops, confirmed[len(res.Applied):]) {
			res.Outcome = OutcomeStopped
			return res, errx.Decorate(errorx.IllegalState.New("the plan changed after it was confirmed"),
				reasons.PreconditionNotMet, "Run the command again to see and confirm the remaining steps.")
		}
		plan = next
	}
}

// withKubeClient puts a kube client in ctx for the hop steps that read the
// cluster through it, unless ctx already carries one.
func withKubeClient(ctx context.Context) (context.Context, error) {
	if _, err := kube.ClientFromContext(ctx); err == nil {
		return ctx, nil
	}
	k, err := newKubeClient()
	if err != nil {
		return ctx, errx.Decorate(errorx.ExternalError.Wrap(err, "failed to create the Kubernetes client"), reasons.K8sAPIFailed,
			"Check that the admin kubeconfig works: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf get nodes")
	}
	return kube.WithKubeClient(ctx, k), nil
}

// recordAtTarget runs the finish gate and records the cluster and the host
// binaries of a cluster at its target. An entry is appended only when a file
// changed or the write failed, so re-running at the target adds nothing.
func recordAtTarget(ctx context.Context, src LiveSources, run WorkflowRunner) error {
	cat, err := src.Catalog()
	if err != nil {
		return errx.Decorate(errorx.IllegalState.Wrap(err, "failed to load the catalog"), reasons.Internal)
	}
	cliArtifact, err := cat.GetHostArtifact(software.CiliumBinaryName)
	if err != nil {
		return errx.Decorate(errorx.IllegalFormat.Wrap(err, "catalog"), reasons.Internal)
	}
	cli := string(cliArtifact.Default)
	write := hopRecord(src, []state.ComponentID{state.ComponentSystem, state.ComponentCluster}, func(st *state.State) {
		recordKubeBinaries(st)
		if at, err := hostBinaryAt(software.CiliumBinaryName, cli); err == nil && at {
			recordSoftware(st, software.CiliumBinaryName, cli)
		}
	})
	intent := models.Intent{Action: models.ActionUpgrade, Target: models.TargetCluster}
	inputs := map[string]string{inputComponent: recordComponent}
	return run(ctx, func() (*automa.Report, error) {
		wf, err := automa.NewWorkflowBuilder().WithId(recordWorkflowID).Steps(
			nodesReadyStep(),
			steps.WaitClusterReadable(clusterReadable(src), readableTimeout, readableInterval),
			steps.RecordState(write),
		).Build()
		if err != nil {
			return nil, errx.Decorate(errorx.InternalError.Wrap(err, "failed to build the record workflow"), reasons.Internal)
		}
		report := wf.Execute(ctx)
		entry := steps.WorkflowEntry(ctx, intent, inputs, report)
		if entry.Outcome == state.OutcomeFailed || entry.NotPersisted || len(entry.Components) > 0 {
			steps.RecordEntry(src.State, entry, report)
		}
		return report, nil
	})
}

const (
	recordWorkflowID = "record-upgraded-cluster"
	recordComponent  = "record"
)

func blockedError(blockers []Blocker) error {
	return errx.Decorate(
		errorx.RejectedOperation.New("the cluster upgrade cannot run: %s", blockerSummary(blockers)),
		reasons.PreconditionNotMet,
		"Run 'solo-provisioner kube cluster upgrade --plan --experimental' to see each blocker, fix them, then run the upgrade again.")
}

func blockerSummary(blockers []Blocker) string {
	parts := make([]string, len(blockers))
	for i, b := range blockers {
		parts[i] = string(b.Code) + ": " + b.Detail
	}
	return strings.Join(parts, "; ")
}

// Describe names the hop for people, such as "cilium 1.18.1 -> 1.18.16".
func (h Hop) Describe() string {
	if h.Kind == KindCharts {
		names := make([]string, len(h.Charts))
		for i, c := range h.Charts {
			names[i] = c.Chart + " " + c.From + " -> " + c.To
		}
		return "charts (" + strings.Join(names, ", ") + ")"
	}
	name := string(h.Kind)
	if h.Component != "" {
		name = h.Component
	}
	return name + " " + h.From + " -> " + h.To
}
