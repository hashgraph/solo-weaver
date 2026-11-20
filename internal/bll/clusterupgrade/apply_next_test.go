// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
)

// scriptedApply replaces the live plan and the hop executors: each call to the
// plan returns the next of plans, and each hop runs hopErr for its index.
type scriptedApply struct {
	plans   []Plan
	planErr map[int]error
	hopErr  map[int]error
	planned int
	hops    []Hop
}

func scriptApply(t *testing.T, plans ...Plan) *scriptedApply {
	t.Helper()
	stubHopFinish(t)
	s := &scriptedApply{plans: plans, planErr: map[int]error{}, hopErr: map[int]error{}}
	origPlan, origHop, origClient := livePlan, applyOneHop, newKubeClient
	t.Cleanup(func() { livePlan, applyOneHop, newKubeClient = origPlan, origHop, origClient })
	livePlan = func(context.Context, LiveSources, Options) (Plan, error) {
		i := s.planned
		s.planned++
		if err := s.planErr[i]; err != nil {
			return Plan{}, err
		}
		require.Less(t, i, len(s.plans), "more plans read than scripted")
		return s.plans[i], nil
	}
	applyOneHop = func(_ context.Context, _ LiveSources, _ *state.HeldLocks, _ WorkflowRunner, hop Hop) error {
		i := len(s.hops)
		s.hops = append(s.hops, hop)
		return s.hopErr[i]
	}
	newKubeClient = func() (*kube.Client, error) { return &kube.Client{}, nil }
	origIdentify, origAt := identifyHostVersions, hostBinaryAt
	t.Cleanup(func() { identifyHostVersions, hostBinaryAt = origIdentify, origAt })
	identifyHostVersions = func(string) ([]string, error) { return nil, nil }
	hostBinaryAt = func(string, string) (bool, error) { return false, nil }
	return s
}

var (
	applyTarget = Target{Kubernetes: "1.36.5", Cilium: "1.20.2"}
	hopA        = ciliumHop(StepPatch, "1.18.1", "1.18.16")
	hopB        = kubeHop(StepPatch, "1.33.4", "1.33.13")
	hopC        = kubeHop(StepMinor, "1.33.13", "1.34.13")
	planABC     = Plan{Target: applyTarget, Hops: []Hop{hopA, hopB, hopC}}
	planBC      = Plan{Target: applyTarget, Hops: []Hop{hopB, hopC}}
	planC       = Plan{Target: applyTarget, Hops: []Hop{hopC}}
	planDone    = Plan{Target: applyTarget, Hops: []Hop{}}
)

func applyWith(t *testing.T, opts ApplyOptions) (ApplyResult, error, string) {
	t.Helper()
	src, dir := liveSources(t, v035Cluster())
	r := &fakeRunner{}
	res, err := Apply(context.Background(), src, holdLocks(t, dir), r.run, opts)
	return res, err, dir
}

func TestApply_OneHop(t *testing.T) {
	s := scriptApply(t, planABC, planBC)

	res, err, _ := applyWith(t, ApplyOptions{})

	require.NoError(t, err)
	assert.Equal(t, OutcomeApplied, res.Outcome)
	assert.Equal(t, []Hop{hopA}, res.Applied)
	assert.Equal(t, []Hop{hopB, hopC}, res.Remaining)
	assert.Equal(t, []Hop{hopA}, s.hops)
}

func TestApply_AllWalksToTheTarget(t *testing.T) {
	s := scriptApply(t, planABC, planBC, planC, planDone)

	res, err, dir := applyWith(t, ApplyOptions{All: true})

	require.NoError(t, err)
	assert.Equal(t, OutcomeWalked, res.Outcome)
	assert.Equal(t, []Hop{hopA, hopB, hopC}, res.Applied)
	assert.Empty(t, res.Remaining)
	assert.Equal(t, []Hop{hopA, hopB, hopC}, s.hops)
	assert.Len(t, historyEntries(t, dir), 1, "the walk ends by recording the cluster at the target")
}

// A walk runs only the hops the operator confirmed.
func TestApply_AllStopsWhenThePlanChangesAfterTheConfirmation(t *testing.T) {
	s := scriptApply(t, planABC, planC)

	res, err, _ := applyWith(t, ApplyOptions{All: true, Confirm: func([]Hop) (bool, error) { return true, nil }})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "the plan changed after it was confirmed")
	assert.Equal(t, OutcomeStopped, res.Outcome)
	assert.Equal(t, []Hop{hopA}, s.hops)
	assert.Equal(t, []Hop{hopC}, res.Remaining)
}

func TestApply_FailedRecordAtTheTargetFails(t *testing.T) {
	scriptApply(t, planDone)
	nodesReadyStep = func() automa.Builder { return noopStep(NodesReadyStepID, true) }

	res, err, _ := applyWith(t, ApplyOptions{})

	require.Error(t, err)
	assert.Equal(t, OutcomeFailed, res.Outcome)
	assert.NotEmpty(t, res.Cause)
}

func TestApply_AllStopsAtTheFirstFailure(t *testing.T) {
	s := scriptApply(t, planABC, planBC)
	s.hopErr[1] = errx.Decorate(errorx.ExternalError.New("kubeadm upgrade apply failed"), reasons.PreconditionNotMet)

	res, err, _ := applyWith(t, ApplyOptions{All: true})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "kubeadm upgrade apply failed")
	assert.Equal(t, OutcomeFailed, res.Outcome)
	assert.Equal(t, []Hop{hopA}, res.Applied)
	assert.Equal(t, []Hop{hopB, hopC}, res.Remaining)
	assert.Contains(t, res.Cause, "kubeadm upgrade apply failed")
	assert.Len(t, s.hops, 2, "no hop runs after a failure")
}

func TestApply_BlockedChangesNothing(t *testing.T) {
	s := scriptApply(t, Plan{Target: applyTarget, Blockers: []Blocker{{Code: BlockerMultipleNodes, Detail: "3 nodes"}}})

	res, err, dir := applyWith(t, ApplyOptions{All: true})

	require.Error(t, err)
	reason, _ := errx.ReasonOf(err)
	assert.Equal(t, reasons.PreconditionNotMet, reason)
	assert.Equal(t, OutcomeBlocked, res.Outcome)
	assert.Len(t, res.Blockers, 1)
	assert.Empty(t, s.hops)
	assert.Empty(t, historyEntries(t, dir))
}

func TestApply_DeclinedChangesNothing(t *testing.T) {
	s := scriptApply(t, planABC)
	var asked []Hop

	res, err, _ := applyWith(t, ApplyOptions{All: true, Confirm: func(hops []Hop) (bool, error) {
		asked = hops
		return false, nil
	}})

	require.Error(t, err)
	assert.Equal(t, OutcomeDeclined, res.Outcome)
	assert.Equal(t, planABC.Hops, asked, "--all asks about every remaining hop")
	assert.Empty(t, s.hops)
}

func TestApply_ConfirmsOnlyTheNextHop(t *testing.T) {
	scriptApply(t, planABC, planBC)
	var asked []Hop

	_, err, _ := applyWith(t, ApplyOptions{Confirm: func(hops []Hop) (bool, error) {
		asked = hops
		return true, nil
	}})

	require.NoError(t, err)
	assert.Equal(t, []Hop{hopA}, asked)
}

// A single hop that worked but whose next plan could not be read still
// succeeds; the walk stops with a hint to re-run.
func TestApply_UnreadableNextPlan(t *testing.T) {
	blocked := Plan{Target: applyTarget, Blockers: []Blocker{{Code: BlockerNotObserved, Detail: "the Kubernetes API server did not answer"}}}

	t.Run("one hop", func(t *testing.T) {
		scriptApply(t, planABC, blocked)
		res, err, _ := applyWith(t, ApplyOptions{})
		require.NoError(t, err)
		assert.Equal(t, OutcomeApplied, res.Outcome)
		assert.Contains(t, res.Cause, "did not answer")
	})
	t.Run("all", func(t *testing.T) {
		scriptApply(t, planABC, blocked)
		res, err, _ := applyWith(t, ApplyOptions{All: true})
		require.Error(t, err)
		assert.Equal(t, OutcomeStopped, res.Outcome)
		assert.Equal(t, []Hop{hopA}, res.Applied)
		reason, _ := errx.ReasonOf(err)
		assert.Equal(t, reasons.PreconditionNotMet, reason)
	})
}

func TestApply_NoProgressIsAnInternalError(t *testing.T) {
	s := scriptApply(t, planABC, planABC)

	res, err, _ := applyWith(t, ApplyOptions{All: true})

	require.Error(t, err)
	reason, _ := errx.ReasonOf(err)
	assert.Equal(t, reasons.Internal, reason)
	assert.Equal(t, OutcomeFailed, res.Outcome)
	assert.Len(t, s.hops, 1)
	assert.Equal(t, planABC.Hops, res.Remaining, "what is left is the last plan read")
	assert.Contains(t, res.Cause, "just applied")
}

func TestApply_StopsAtTheHopCap(t *testing.T) {
	// Each plan names a new hop but never gets shorter.
	plans := []Plan{}
	for i := 0; i < 8; i++ {
		plans = append(plans, Plan{Target: applyTarget, Hops: []Hop{kubeHop(StepPatch, "1.33.4", "1.33."+string(rune('a'+i)))}})
	}
	s := scriptApply(t, plans...)

	_, err, _ := applyWith(t, ApplyOptions{All: true})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not reach its target in 3 hops")
	assert.Len(t, s.hops, 3)
}

// At the target the run records the cluster, and a second run adds nothing.
func TestApply_AtTargetRecordsOnlyWhatChanged(t *testing.T) {
	scriptApply(t, planDone, planDone)
	stubKubernetesApply(t, at("1.33.4", "1.33.4"), false)
	origAt := hostBinaryAt
	t.Cleanup(func() { hostBinaryAt = origAt })
	hostBinaryAt = func(string, string) (bool, error) { return true, nil }
	src, dir := liveSources(t, v035Cluster())
	locks := holdLocks(t, dir)
	r := &fakeRunner{}

	res, err := Apply(context.Background(), src, locks, r.run, ApplyOptions{})
	require.NoError(t, err)
	assert.Equal(t, OutcomeAtTarget, res.Outcome)
	assert.Equal(t, []string{NodesReadyStepID, steps.WaitClusterReadableStepID, steps.RecordStateStepID}, r.stepIDs(t))
	entries := historyEntries(t, dir)
	require.Len(t, entries, 1)
	assert.ElementsMatch(t, []state.ChangedFile{state.ComponentSystem.File(), state.ComponentCluster.File()}, entries[0].Components)
	history := readStateFile(t, dir, "action_history.yaml")
	assert.Contains(t, history, "component: cluster")
	assert.Contains(t, history, "step: record")

	_, err = Apply(context.Background(), src, locks, (&fakeRunner{}).run, ApplyOptions{})
	require.NoError(t, err)
	assert.Len(t, historyEntries(t, dir), 1, "nothing changed, so no entry")
}

func TestApply_RefusesLocksThatDoNotCoverIt(t *testing.T) {
	s := scriptApply(t, planABC)
	src, dir := liveSources(t, v035Cluster())
	only, err := state.AcquireHeldLocks(dir, []state.ComponentID{state.ComponentCluster}, 0)
	require.NoError(t, err)
	t.Cleanup(only.Release)

	_, err = Apply(context.Background(), src, only, (&fakeRunner{}).run, ApplyOptions{})

	require.Error(t, err)
	reason, _ := errx.ReasonOf(err)
	assert.Equal(t, reasons.Internal, reason)
	assert.Zero(t, s.planned, "nothing is read without the locks")
}

func TestApply_NeedsARunner(t *testing.T) {
	src, dir := liveSources(t, v035Cluster())
	_, err := Apply(context.Background(), src, holdLocks(t, dir), nil, ApplyOptions{})
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.IllegalArgument))
}

func TestApplyHop_UnknownKindIsInternal(t *testing.T) {
	src, dir := liveSources(t, v035Cluster())
	err := ApplyHop(context.Background(), src, holdLocks(t, dir), (&fakeRunner{}).run, Hop{Kind: "teleport"})
	require.Error(t, err)
	reason, _ := errx.ReasonOf(err)
	assert.Equal(t, reasons.Internal, reason)

	_, err = HopChecks(clusterShape("1.33.4", "1.18.1"), fixtureMatrix(), Hop{Kind: "teleport"})
	require.Error(t, err)
}

func TestDryRun_ChecksTheNextHop(t *testing.T) {
	src, dir := liveSources(t, v035Cluster())

	res, err := DryRun(context.Background(), src, Options{})

	require.NoError(t, err)
	require.NotNil(t, res.Next)
	assert.Equal(t, res.Plan.Hops[0], *res.Next)
	assert.NotEmpty(t, res.Checks)
	assert.Empty(t, stateFiles(t, dir), "a dry run changes nothing")
}
