// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"context"
	"strings"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8sversion "k8s.io/apimachinery/pkg/version"

	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
)

func refreshHop(bumps ...ChartBump) Hop { return chartsHop(StepRefresh, bumps...) }

func TestChartsHopChecks_Pass(t *testing.T) {
	for _, tc := range []struct {
		name  string
		shape rsl.ClusterShape
		hop   Hop
		n     int
	}{
		{"refresh", clusterShape("1.36.5", "1.20.2", withChart("alloy", "1.4.0"), withChart("metrics-server", "3.13.0")),
			refreshHop(bump("alloy", "1.4.0", "1.13.1"), bump("metrics-server", "3.13.0", "3.14.0")), 5},
		{"refresh that stopped part way", clusterShape("1.36.5", "1.20.2", withChart("alloy", "1.13.1"), withChart("metrics-server", "3.13.0")),
			refreshHop(bump("alloy", "1.4.0", "1.13.1"), bump("metrics-server", "3.13.0", "3.14.0")), 5},
		{"coupled before the next minor", clusterShape("1.35.10", "1.20.2", withChart("metallb", "0.15.2")),
			coupledHop("1.36", bump("metallb", "0.15.2", "0.16.1")), 6},
		{"coupled for the current minor", clusterShape("1.36.5", "1.20.2", withChart("metallb", "0.15.2")),
			coupledHop("1.36", bump("metallb", "0.15.2", "0.16.1")), 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checks, err := ChartsHopChecks(tc.shape, fixtureMatrix(), tc.hop)
			require.NoError(t, err)
			assert.Empty(t, checks.Failed())
			assert.NoError(t, checks.Err())
			assert.Len(t, checks, tc.n)
		})
	}
}

func TestChartsHopChecks_Fail(t *testing.T) {
	unread := clusterShape("1.36.5", "1.20.2", withChart("alloy", "1.4.0"))
	unread.Observed = false
	nodes := clusterShape("1.36.5", "1.20.2", withChart("alloy", "1.4.0"))
	nodes.Kubernetes.NodeCount, nodes.Kubernetes.UnexpectedNodes = 2, true
	alloyRefresh := refreshHop(bump("alloy", "1.4.0", "1.13.1"))

	for _, tc := range []struct {
		name  string
		shape rsl.ClusterShape
		hop   Hop
		check string
	}{
		{"cluster not read", unread, alloyRefresh, "the cluster was read"},
		{"more nodes", nodes, alloyRefresh, "one node"},
		{"chart at another version", clusterShape("1.36.5", "1.20.2", withChart("alloy", "1.3.0")), alloyRefresh, "every chart is where the hop starts"},
		{"chart under a legacy identity", clusterShape("1.36.5", "1.20.2", withLegacy("alloy", "1.4.0")), alloyRefresh, "every chart is where the hop starts"},
		{"chart not installed", clusterShape("1.36.5", "1.20.2"), alloyRefresh, "every chart is where the hop starts"},
		{"refresh target not the default", clusterShape("1.36.5", "1.20.2", withChart("metallb", "0.15.2")),
			refreshHop(bump("metallb", "0.15.2", "0.15.3")), "every target is the catalog's"},
		{"coupled target not the minimum", clusterShape("1.34.13", "1.20.2", withChart("metallb", "0.15.2")),
			coupledHop("1.35", bump("metallb", "0.15.2", "0.16.1")), "every target is the catalog's"},
		{"chart this binary does not upgrade", clusterShape("1.36.5", "1.20.2"),
			refreshHop(bump("cilium", "1.18.1", "1.20.2")), "every chart can be upgraded"},
		{"coupled for a minor two away", clusterShape("1.34.13", "1.20.2", withChart("metallb", "0.15.2")),
			coupledHop("1.36", bump("metallb", "0.15.2", "0.16.1")), "the hop is for this cluster's Kubernetes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checks, err := ChartsHopChecks(tc.shape, fixtureMatrix(), tc.hop)
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

func TestChartsHopChecks_RejectsOtherHops(t *testing.T) {
	for _, hop := range []Hop{
		ciliumHop(StepPatch, "1.18.1", "1.18.16"),
		{Kind: KindCharts, Step: StepPatch, Charts: []ChartBump{bump("alloy", "1.4.0", "1.13.1")}},
		refreshHop(),
		refreshHop(bump("alloy", "1.4.0", "1.13.1"), bump("alloy", "1.4.0", "1.13.1")),
		refreshHop(bump("alloy", "1.4", "1.13.1")),
		refreshHop(bump("alloy", "1.4.0", "latest")),
		coupledHop("", bump("metallb", "0.15.2", "0.16.1")),
		coupledHop("1.36.5", bump("metallb", "0.15.2", "0.16.1")),
	} {
		_, err := ChartsHopChecks(clusterShape("1.36.5", "1.20.2"), fixtureMatrix(), hop)
		require.Error(t, err)
		assert.True(t, errorx.IsOfType(err, errorx.IllegalArgument), err.Error())
	}
}

func TestChartsHopChecks_InvalidMatrix(t *testing.T) {
	_, err := ChartsHopChecks(clusterShape("1.36.5", "1.20.2"), Matrix{}, refreshHop(bump("alloy", "1.4.0", "1.13.1")))
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.IllegalFormat), err.Error())
}

// stubChartsApply records the bumps each hop workflow is built with.
func stubChartsApply(t *testing.T) *[][]steps.ChartBump {
	t.Helper()
	orig := chartHopWorkflow
	t.Cleanup(func() { chartHopWorkflow = orig })
	var built [][]steps.ChartBump
	chartHopWorkflow = func(bumps []steps.ChartBump) *automa.WorkflowBuilder {
		built = append(built, bumps)
		return automa.NewWorkflowBuilder().WithId("charts").
			Steps(automa.NewStepBuilder().WithId("noop").WithExecute(func(_ context.Context, stp automa.Step) *automa.Report {
				return automa.SuccessReport(stp)
			}))
	}
	return &built
}

var v035Refresh = refreshHop(bump("metallb", "0.15.2", "0.16.1"), bump("metrics-server", "3.13.0", "3.14.0"))

func TestApplyChartsHop_RunsTheHopAndRecords(t *testing.T) {
	built := stubChartsApply(t)
	src, dir := liveSources(t, v035Cluster())
	r := &fakeRunner{}

	require.NoError(t, ApplyChartsHop(context.Background(), src, r.run, v035Refresh))

	assert.Equal(t, []string{"charts"}, r.ran)
	assert.Equal(t, [][]steps.ChartBump{{{Chart: "metallb", From: "0.15.2", To: "0.16.1"}, {Chart: "metrics-server", From: "3.13.0", To: "3.14.0"}}}, *built)
	assert.Contains(t, readStateFile(t, dir, "cluster.yaml"), "releases:")
	history := readStateFile(t, dir, "action_history.yaml")
	assert.Contains(t, history, "component: charts")
	assert.Contains(t, history, "metallb=0.15.2->0.16.1,metrics-server=3.13.0->3.14.0")
	assert.NotContains(t, history, "kubernetes:")
}

func TestApplyChartsHop_CoupledRecordsItsMinor(t *testing.T) {
	stubChartsApply(t)
	cs := v035Cluster()
	cs.ServerVersion = k8sversion.Info{GitVersion: "v1.35.10"}
	cs.KubeletVersion = "1.35.10"
	cs.CiliumAgentVersion = "1.20.2"
	cs.Releases["cilium"] = deployed("cilium", "1.20.2")
	src, dir := liveSources(t, cs)
	r := &fakeRunner{}

	require.NoError(t, ApplyChartsHop(context.Background(), src, r.run, coupledHop("1.36", bump("metallb", "0.15.2", "0.16.1"))))

	history := readStateFile(t, dir, "action_history.yaml")
	assert.Contains(t, history, "kubernetes: \"1.36\"")
	assert.Contains(t, history, "step: coupled")
}

func TestApplyChartsHop_DoneHopDoesNothing(t *testing.T) {
	built := stubChartsApply(t)
	cs := v035Cluster()
	cs.Releases["metallb"] = deployed("metallb", "0.16.1")
	cs.Releases["metrics-server"] = deployed("metrics-server", "3.14.0")
	src, dir := liveSources(t, cs)
	r := &fakeRunner{}

	require.NoError(t, ApplyChartsHop(context.Background(), src, r.run, v035Refresh))

	assert.Empty(t, r.ran)
	assert.Empty(t, *built)
	assert.Empty(t, stateFiles(t, dir))
}

func TestApplyChartsHop_FailedCheckChangesNothing(t *testing.T) {
	stubChartsApply(t)
	src, dir := liveSources(t, v035Cluster())
	r := &fakeRunner{}

	err := ApplyChartsHop(context.Background(), src, r.run, refreshHop(bump("metallb", "0.14.0", "0.16.1")))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "every chart is where the hop starts")
	assert.Empty(t, r.ran)
	assert.Empty(t, stateFiles(t, dir))
}

func TestApplyChartsHop_FailedHopRecordsTheFailure(t *testing.T) {
	stubChartsApply(t)
	src, dir := liveSources(t, v035Cluster())
	r := &fakeRunner{err: errorx.ExternalError.New("helm upgrade failed")}

	err := ApplyChartsHop(context.Background(), src, r.run, v035Refresh)

	require.Error(t, err)
	assert.NotContains(t, stateFiles(t, dir), "cluster.yaml")
	history := readStateFile(t, dir, "action_history.yaml")
	assert.Contains(t, history, "outcome: failed")
	assert.True(t, strings.Contains(history, "component: charts"))
}

func TestApplyChartsHop_FailsFastOnTheClusterLock(t *testing.T) {
	stubChartsApply(t)
	src, dir := liveSources(t, v035Cluster())
	release, err := state.AcquireComponentLocks(dir, []state.ComponentID{state.ComponentCluster}, 0)
	require.NoError(t, err)
	defer release()
	r := &fakeRunner{}

	require.Error(t, ApplyChartsHop(context.Background(), src, r.run, v035Refresh))

	assert.Empty(t, r.ran)
	entries, err := state.ReadActionHistory(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestApplyChartsHop_NeedsARunner(t *testing.T) {
	src, _ := liveSources(t, v035Cluster())
	err := ApplyChartsHop(context.Background(), src, nil, v035Refresh)
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.IllegalArgument))
}
