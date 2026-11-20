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
	"helm.sh/helm/v3/pkg/release"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
)

func neReplace(from string) Hop {
	return Hop{Kind: KindStructural, Step: StepReplace, Component: "node-exporter", From: from, To: "4.59.0"}
}

func esoMigrate(from string) Hop {
	return Hop{Kind: KindStructural, Step: StepMigrate, Component: "external-secrets", From: from, To: "2.13.0"}
}

func withMissing(name string) shapeOpt {
	return func(s *rsl.ClusterShape) { s.Charts[name] = rsl.ChartShape{Missing: true} }
}

// withSwapStopped is a node-exporter swap that stopped after its uninstall:
// no release, and the record still names the legacy chart at recorded.
func withSwapStopped(recorded string) shapeOpt {
	return func(s *rsl.ClusterShape) {
		s.Charts["node-exporter"] = rsl.ChartShape{Missing: true, Recorded: recorded, RecordedIdentity: rsl.IdentityLegacy}
	}
}

func withStatus(name string, status release.Status) shapeOpt {
	return func(s *rsl.ClusterShape) {
		ch := s.Charts[name]
		ch.Status = status
		s.Charts[name] = ch
	}
}

func TestStructuralHopChecks_Pass(t *testing.T) {
	for _, tc := range []struct {
		name  string
		shape rsl.ClusterShape
		hop   Hop
	}{
		{"node-exporter swap", clusterShape("1.33.4", "1.18.1", withLegacy("node-exporter", "4.4.9")), neReplace("4.4.9")},
		{"node-exporter swap that stopped after the uninstall", clusterShape("1.33.4", "1.18.1", withSwapStopped("4.4.9")), neReplace("4.4.9")},
		{"External Secrets migration", clusterShape("1.33.4", "1.18.1", withLegacy("external-secrets", "0.20.2")), esoMigrate("0.20.2")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checks, err := StructuralHopChecks(tc.shape, fixtureMatrix(), tc.hop)
			require.NoError(t, err)
			assert.Empty(t, checks.Failed())
			assert.NoError(t, checks.Err())
			assert.Len(t, checks, 5)
		})
	}
}

func TestStructuralHopChecks_Fail(t *testing.T) {
	unread := clusterShape("1.33.4", "1.18.1", withLegacy("node-exporter", "4.4.9"))
	unread.Observed = false
	nodes := clusterShape("1.33.4", "1.18.1", withLegacy("node-exporter", "4.4.9"))
	nodes.Kubernetes.NodeCount, nodes.Kubernetes.UnexpectedNodes = 2, true

	for _, tc := range []struct {
		name  string
		shape rsl.ClusterShape
		hop   Hop
		check string
	}{
		{"cluster not read", unread, neReplace("4.4.9"), "the cluster was read"},
		{"more nodes", nodes, neReplace("4.4.9"), "one node"},
		{"legacy chart at another version", clusterShape("1.33.4", "1.18.1", withLegacy("node-exporter", "4.4.8")), neReplace("4.4.9"), "the chart is where the hop starts"},
		{"current chart, not the target", clusterShape("1.33.4", "1.18.1", withChart("node-exporter", "4.58.0")), neReplace("4.4.9"), "the chart is where the hop starts"},
		{"no External Secrets to migrate", clusterShape("1.33.4", "1.18.1", withMissing("external-secrets")), esoMigrate("0.20.2"), "the chart is where the hop starts"},
		{"node-exporter never installed", clusterShape("1.33.4", "1.18.1", withMissing("node-exporter")), neReplace("4.4.9"), "the chart is where the hop starts"},
		{"swap stopped at another version", clusterShape("1.33.4", "1.18.1", withSwapStopped("4.4.8")), neReplace("4.4.9"), "the chart is where the hop starts"},
		{"legacy release failed", clusterShape("1.33.4", "1.18.1", withLegacy("external-secrets", "0.20.2"),
			withStatus("external-secrets", release.StatusFailed)), esoMigrate("0.20.2"), "the release is deployed"},
		{"chart not in the cluster", clusterShape("1.33.4", "1.18.1"), esoMigrate("0.20.2"), "the chart is where the hop starts"},
		{"target is not the catalog default", clusterShape("1.33.4", "1.18.1", withLegacy("external-secrets", "0.20.2")),
			Hop{Kind: KindStructural, Step: StepMigrate, Component: "external-secrets", From: "0.20.2", To: "2.12.0"}, "the target is the catalog default"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checks, err := StructuralHopChecks(tc.shape, fixtureMatrix(), tc.hop)
			require.NoError(t, err)
			failed := checks.Failed()
			require.NotEmpty(t, failed)
			names := make([]string, len(failed))
			for i, c := range failed {
				names[i] = c.Name
				assert.NotEmpty(t, c.Detail)
			}
			assert.Contains(t, names, tc.check)
			err = checks.Err()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.check)
		})
	}
}

func TestStructuralHopChecks_RejectsOtherHops(t *testing.T) {
	for _, hop := range []Hop{
		ciliumHop(StepPatch, "1.18.1", "1.18.16"),
		{Kind: KindStructural, Step: StepMigrate, Component: "node-exporter", From: "4.4.9", To: "4.59.0"},
		{Kind: KindStructural, Step: StepReplace, Component: "external-secrets", From: "0.20.2", To: "2.13.0"},
		{Kind: KindStructural, Step: StepReplace, Component: "alloy", From: "1.4.0", To: "1.13.1"},
		neReplace(""),
		{Kind: KindStructural, Step: StepReplace, Component: "node-exporter", From: "4.4.9", To: "latest"},
	} {
		_, err := StructuralHopChecks(clusterShape("1.33.4", "1.18.1"), fixtureMatrix(), hop)
		require.Error(t, err)
		assert.True(t, errorx.IsOfType(err, errorx.IllegalArgument), err.Error())
		reason, _ := errx.ReasonOf(err)
		assert.Equal(t, reasons.Internal, reason)
	}
}

func TestStructuralHopChecks_InvalidMatrix(t *testing.T) {
	_, err := StructuralHopChecks(clusterShape("1.33.4", "1.18.1"), Matrix{}, neReplace("4.4.9"))
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.IllegalFormat), err.Error())
}

// stubStructuralApply stubs the hop workflows, whose step fails when fail is
// set.
func stubStructuralApply(t *testing.T, fail bool) {
	t.Helper()
	stubHopFinish(t)
	origNE, origESO := nodeExporterReplaceWorkflow, externalSecretsMigrateWorkflow
	t.Cleanup(func() { nodeExporterReplaceWorkflow, externalSecretsMigrateWorkflow = origNE, origESO })
	hop := func(id string) *automa.WorkflowBuilder {
		return automa.NewWorkflowBuilder().WithId(id).Steps(noopStep("structural-hop", fail))
	}
	nodeExporterReplaceWorkflow = func() *automa.WorkflowBuilder { return hop("ne-replace") }
	externalSecretsMigrateWorkflow = func(to string) *automa.WorkflowBuilder { return hop("eso-" + to) }
}

// legacyCluster is a v0.35 cluster that also runs the Bitnami node-exporter
// and External Secrets 0.20.2.
func legacyCluster() state.ClusterState {
	cs := v035Cluster()
	cs.Releases["node-exporter"] = deployed("node-exporter", "4.4.9")
	cs.Releases["external-secrets"] = deployed("external-secrets", "0.20.2")
	return cs
}

func TestApplyStructuralHop_RunsTheHopAndRecords(t *testing.T) {
	for _, tc := range []struct {
		hop       Hop
		workflow  string
		component string
	}{
		{neReplace("4.4.9"), "ne-replace", "component: node-exporter"},
		{esoMigrate("0.20.2"), "eso-2.13.0", "component: external-secrets"},
	} {
		t.Run(tc.workflow, func(t *testing.T) {
			stubStructuralApply(t, false)
			src, dir := liveSources(t, legacyCluster())
			r := &fakeRunner{}

			require.NoError(t, ApplyStructuralHop(context.Background(), src, holdLocks(t, dir), r.run, tc.hop))

			assert.Equal(t, []string{tc.workflow}, r.ran())
			assert.Equal(t, []string{"structural-hop", NodesReadyStepID, steps.WaitClusterReadableStepID, steps.RecordStateStepID},
				r.stepIDs(t))
			assert.Contains(t, readStateFile(t, dir, "cluster.yaml"), "releases:")
			assert.NotContains(t, stateFiles(t, dir), "system.yaml")
			entries := historyEntries(t, dir)
			require.Len(t, entries, 1)
			assert.Equal(t, state.OutcomeSucceeded, entries[0].Outcome)
			assert.Equal(t, []state.ChangedFile{state.ComponentCluster.File()}, entries[0].Components)
			history := readStateFile(t, dir, "action_history.yaml")
			assert.Contains(t, history, tc.component)
			assert.Contains(t, history, "step: "+string(tc.hop.Step))
			assert.Contains(t, history, "to: "+tc.hop.To)
		})
	}
}

func TestApplyStructuralHop_DoneHopDoesNothing(t *testing.T) {
	stubStructuralApply(t, false)
	cs := v035Cluster()
	cs.Releases["node-exporter"] = deployed("prometheus-node-exporter", "4.59.0")
	src, dir := liveSources(t, cs)
	r := &fakeRunner{}

	require.NoError(t, ApplyStructuralHop(context.Background(), src, holdLocks(t, dir), r.run, neReplace("4.4.9")))

	assert.Empty(t, r.ran())
	assert.Empty(t, stateFiles(t, dir))
}

// A release at the target whose last Helm revision failed is not done: the
// hop is refused with what to do, not skipped.
func TestApplyStructuralHop_FailedReleaseAtTheTargetIsNotDone(t *testing.T) {
	stubStructuralApply(t, false)
	cs := legacyCluster()
	failed := deployed("external-secrets", "2.13.0")
	failed.Status = release.StatusFailed
	cs.Releases["external-secrets"] = failed
	src, dir := liveSources(t, cs)
	r := &fakeRunner{}

	err := ApplyStructuralHop(context.Background(), src, holdLocks(t, dir), r.run, esoMigrate("0.20.2"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "the release is deployed")
	assert.Empty(t, r.ran())
}

func TestApplyStructuralHop_FailedCheckChangesNothing(t *testing.T) {
	stubStructuralApply(t, false)
	src, dir := liveSources(t, legacyCluster())
	r := &fakeRunner{}

	err := ApplyStructuralHop(context.Background(), src, holdLocks(t, dir), r.run, esoMigrate("0.19.0"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "the chart is where the hop starts")
	reason, _ := errx.ReasonOf(err)
	assert.Equal(t, reasons.PreconditionNotMet, reason)
	assert.Empty(t, r.ran())
	assert.Empty(t, stateFiles(t, dir))
}

func TestApplyStructuralHop_FailedHopRecordsOnlyTheFailure(t *testing.T) {
	stubStructuralApply(t, true)
	src, dir := liveSources(t, legacyCluster())
	r := &fakeRunner{}

	err := ApplyStructuralHop(context.Background(), src, holdLocks(t, dir), r.run, esoMigrate("0.20.2"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "structural-hop failed")
	assert.NotContains(t, stateFiles(t, dir), "cluster.yaml")
	entries := historyEntries(t, dir)
	require.Len(t, entries, 1)
	assert.Equal(t, state.OutcomeFailed, entries[0].Outcome)
	assert.Empty(t, entries[0].Components)
}

// A record that cannot be written does not fail a hop that ran.
func TestApplyStructuralHop_RecordFailureIsAWarning(t *testing.T) {
	stubStructuralApply(t, false)
	src, dir := liveSources(t, legacyCluster())
	src.State = failingPersist{src.State}
	r := &fakeRunner{}

	require.NoError(t, ApplyStructuralHop(context.Background(), src, holdLocks(t, dir), r.run, neReplace("4.4.9")))

	entries := historyEntries(t, dir)
	require.Len(t, entries, 1)
	assert.Equal(t, state.OutcomeSucceeded, entries[0].Outcome)
	assert.True(t, entries[0].NotPersisted)
}

func TestApplyStructuralHop_RefusesLocksThatDoNotCoverIt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		locks func(t *testing.T, dir string) *state.HeldLocks
	}{
		{"no locks", func(*testing.T, string) *state.HeldLocks { return nil }},
		{"only the system lock", func(t *testing.T, dir string) *state.HeldLocks {
			l, err := state.AcquireHeldLocks(dir, []state.ComponentID{state.ComponentSystem}, 0)
			require.NoError(t, err)
			t.Cleanup(l.Release)
			return l
		}},
		{"locks in another directory", func(t *testing.T, _ string) *state.HeldLocks { return holdLocks(t, t.TempDir()) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubStructuralApply(t, false)
			src, dir := liveSources(t, legacyCluster())
			r := &fakeRunner{}

			err := ApplyStructuralHop(context.Background(), src, tc.locks(t, dir), r.run, neReplace("4.4.9"))

			require.Error(t, err)
			reason, _ := errx.ReasonOf(err)
			assert.Equal(t, reasons.Internal, reason)
			assert.Empty(t, r.ran(), "no workflow may run without the locks")
			assert.Empty(t, historyEntries(t, dir))
		})
	}
}

func TestApplyStructuralHop_UnreachableClusterIsRefused(t *testing.T) {
	stubStructuralApply(t, false)
	src, dir := liveSources(t, legacyCluster())
	src.Probe = probeReturns(reality.NotConfigured)
	r := &fakeRunner{}

	err := ApplyStructuralHop(context.Background(), src, holdLocks(t, dir), r.run, neReplace("4.4.9"))

	require.Error(t, err)
	reason, _ := errx.ReasonOf(err)
	assert.Equal(t, reasons.PreconditionNotMet, reason)
	assert.Empty(t, r.ran())
	assert.Empty(t, historyEntries(t, dir))
}

func TestApplyStructuralHop_NeedsARunner(t *testing.T) {
	src, dir := liveSources(t, legacyCluster())
	err := ApplyStructuralHop(context.Background(), src, holdLocks(t, dir), nil, neReplace("4.4.9"))
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.IllegalArgument))
}
