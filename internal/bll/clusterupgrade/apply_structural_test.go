// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
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

func TestStructuralHopChecks_Pass(t *testing.T) {
	for _, tc := range []struct {
		name  string
		shape rsl.ClusterShape
		hop   Hop
	}{
		{"node-exporter swap", clusterShape("1.33.4", "1.18.1", withLegacy("node-exporter", "4.4.9")), neReplace("4.4.9")},
		{"node-exporter swap that stopped after the uninstall", clusterShape("1.33.4", "1.18.1", withMissing("node-exporter")), neReplace("4.4.9")},
		{"External Secrets migration", clusterShape("1.33.4", "1.18.1", withLegacy("external-secrets", "0.20.2")), esoMigrate("0.20.2")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checks, err := StructuralHopChecks(tc.shape, fixtureMatrix(), tc.hop)
			require.NoError(t, err)
			assert.Empty(t, checks.Failed())
			assert.NoError(t, checks.Err())
			assert.Len(t, checks, 4)
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
	}
}

func TestStructuralHopChecks_InvalidMatrix(t *testing.T) {
	_, err := StructuralHopChecks(clusterShape("1.33.4", "1.18.1"), Matrix{}, neReplace("4.4.9"))
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.IllegalFormat), err.Error())
}

func stubStructuralApply(t *testing.T) {
	t.Helper()
	origNE, origESO := nodeExporterReplaceWorkflow, externalSecretsMigrateWorkflow
	t.Cleanup(func() { nodeExporterReplaceWorkflow, externalSecretsMigrateWorkflow = origNE, origESO })
	noop := func(id string) *automa.WorkflowBuilder {
		return automa.NewWorkflowBuilder().WithId(id).
			Steps(automa.NewStepBuilder().WithId("noop").WithExecute(func(_ context.Context, stp automa.Step) *automa.Report {
				return automa.SuccessReport(stp)
			}))
	}
	nodeExporterReplaceWorkflow = func() *automa.WorkflowBuilder { return noop("ne-replace") }
	externalSecretsMigrateWorkflow = func(to string) *automa.WorkflowBuilder { return noop("eso-" + to) }
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
			stubStructuralApply(t)
			src, dir := liveSources(t, legacyCluster())
			r := &fakeRunner{}

			require.NoError(t, ApplyStructuralHop(context.Background(), src, r.run, tc.hop))

			assert.Equal(t, []string{tc.workflow}, r.ran)
			assert.Contains(t, readStateFile(t, dir, "cluster.yaml"), "releases:")
			assert.NotContains(t, stateFiles(t, dir), "system.yaml")
			history := readStateFile(t, dir, "action_history.yaml")
			assert.Contains(t, history, tc.component)
			assert.Contains(t, history, "to: "+tc.hop.To)
		})
	}
}

func TestApplyStructuralHop_DoneHopDoesNothing(t *testing.T) {
	stubStructuralApply(t)
	cs := v035Cluster()
	cs.Releases["node-exporter"] = deployed("prometheus-node-exporter", "4.59.0")
	src, dir := liveSources(t, cs)
	r := &fakeRunner{}

	require.NoError(t, ApplyStructuralHop(context.Background(), src, r.run, neReplace("4.4.9")))

	assert.Empty(t, r.ran)
	assert.Empty(t, stateFiles(t, dir))
}

func TestApplyStructuralHop_FailedCheckChangesNothing(t *testing.T) {
	stubStructuralApply(t)
	src, dir := liveSources(t, legacyCluster())
	r := &fakeRunner{}

	err := ApplyStructuralHop(context.Background(), src, r.run, esoMigrate("0.19.0"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "the chart is where the hop starts")
	assert.Empty(t, r.ran)
	assert.Empty(t, stateFiles(t, dir))
}

func TestApplyStructuralHop_FailedHopRecordsTheFailure(t *testing.T) {
	stubStructuralApply(t)
	src, dir := liveSources(t, legacyCluster())
	r := &fakeRunner{err: errorx.ExternalError.New("helm upgrade failed")}

	err := ApplyStructuralHop(context.Background(), src, r.run, esoMigrate("0.20.2"))

	require.Error(t, err)
	assert.NotContains(t, stateFiles(t, dir), "cluster.yaml")
	assert.Contains(t, readStateFile(t, dir, "action_history.yaml"), "outcome: failed")
}

func TestApplyStructuralHop_FailsFastOnTheClusterLock(t *testing.T) {
	stubStructuralApply(t)
	src, dir := liveSources(t, legacyCluster())
	release, err := state.AcquireComponentLocks(dir, []state.ComponentID{state.ComponentCluster}, 0)
	require.NoError(t, err)
	defer release()
	r := &fakeRunner{}

	require.Error(t, ApplyStructuralHop(context.Background(), src, r.run, neReplace("4.4.9")))

	assert.Empty(t, r.ran)
	entries, err := state.ReadActionHistory(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestApplyStructuralHop_NeedsARunner(t *testing.T) {
	src, _ := liveSources(t, legacyCluster())
	err := ApplyStructuralHop(context.Background(), src, nil, neReplace("4.4.9"))
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.IllegalArgument))
}
