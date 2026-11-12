// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/state"
)

func ciliumPatch() Hop { return ciliumHop(StepPatch, "1.18.1", "1.18.16") }

func TestCiliumHopChecks_Pass(t *testing.T) {
	for _, tc := range []struct {
		name  string
		shape string
		hop   Hop
	}{
		{"patch", "1.18.1", ciliumPatch()},
		{"minor", "1.18.16", ciliumHop(StepMinor, "1.18.16", "1.19.10")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checks, err := CiliumHopChecks(clusterShape("1.33.4", tc.shape), fixtureMatrix(), tc.hop)
			require.NoError(t, err)
			assert.Empty(t, checks.Failed())
			assert.NoError(t, checks.Err())
			assert.Len(t, checks, 7)
		})
	}
}

func TestCiliumHopChecks_Fail(t *testing.T) {
	cases := []struct {
		name  string
		kube  string
		hop   Hop
		edit  func(s *shapeEdit)
		check string
	}{
		{"cluster not read", "1.33.4", ciliumPatch(), func(s *shapeEdit) { s.unobserved = true }, "the cluster was read"},
		{"more nodes", "1.33.4", ciliumPatch(), func(s *shapeEdit) { s.nodes = 3 }, "one node"},
		{"Cilium elsewhere", "1.33.4", ciliumHop(StepPatch, "1.18.0", "1.18.16"), nil, "Cilium is where the hop starts"},
		{"agent differs", "1.33.4", ciliumPatch(), func(s *shapeEdit) { s.agent = "1.17.0" }, "the Cilium agent matches its chart"},
		{"agent missing", "1.33.4", ciliumPatch(), func(s *shapeEdit) { s.agent = "" }, "the Cilium agent matches its chart"},
		{"target not shipped", "1.33.4", ciliumHop(StepPatch, "1.18.1", "1.18.15"), nil, "the target is a catalog version"},
		{"two minors", "1.33.4", ciliumHop(StepMinor, "1.18.1", "1.20.2"), nil, "one step at a time"},
		{"downgrade", "1.33.4", ciliumHop(StepPatch, "1.18.1", "1.18.0"), nil, "one step at a time"},
		{"target too new for Kubernetes", "1.30.2", ciliumHop(StepMinor, "1.18.1", "1.19.10"), nil, "the target runs on this Kubernetes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := shapeEdit{agent: "1.18.1", nodes: 1}
			if tc.edit != nil {
				tc.edit(&e)
			}
			s := clusterShape(tc.kube, "1.18.1")
			s.Observed = !e.unobserved
			s.Kubernetes.NodeCount, s.Kubernetes.UnexpectedNodes = e.nodes, e.nodes > 1
			s.Cilium.Agent, s.Cilium.Mismatch = e.agent, e.agent != "" && e.agent != "1.18.1"

			checks, err := CiliumHopChecks(s, fixtureMatrix(), tc.hop)
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

type shapeEdit struct {
	unobserved bool
	nodes      int
	agent      string
}

func TestCiliumHopChecks_RejectsOtherHops(t *testing.T) {
	for _, hop := range []Hop{
		kubeHop(StepMinor, "1.33.13", "1.34.13"),
		{Kind: KindCilium, Step: StepResume, From: "1.18.1", To: "1.18.16"},
		ciliumHop(StepPatch, "1.18", "1.18.16"),
		ciliumHop(StepPatch, "1.18.1", "latest"),
	} {
		_, err := CiliumHopChecks(clusterShape("1.33.4", "1.18.1"), fixtureMatrix(), hop)
		require.Error(t, err)
		assert.True(t, errorx.IsOfType(err, errorx.IllegalArgument), err.Error())
	}
}

// fakeRunner records the workflows it is asked to run.
type fakeRunner struct {
	ran []string
	err error
}

func (f *fakeRunner) run(_ context.Context, wb *automa.WorkflowBuilder) error {
	wf, err := wb.Build()
	if err != nil {
		return err
	}
	f.ran = append(f.ran, wf.Id())
	return f.err
}

func stubCiliumApply(t *testing.T, cliAt bool) {
	t.Helper()
	origAt, origCLI, origHop := hostBinaryAt, ciliumCLIWorkflow, ciliumHopWorkflow
	t.Cleanup(func() { hostBinaryAt, ciliumCLIWorkflow, ciliumHopWorkflow = origAt, origCLI, origHop })
	hostBinaryAt = func(string, string) (bool, error) { return cliAt, nil }
	noop := func() automa.Builder {
		return automa.NewStepBuilder().WithId("noop").WithExecute(func(_ context.Context, stp automa.Step) *automa.Report {
			return automa.SuccessReport(stp)
		})
	}
	ciliumCLIWorkflow = func(version string) *automa.WorkflowBuilder {
		return automa.NewWorkflowBuilder().WithId("cli-" + version).Steps(noop())
	}
	ciliumHopWorkflow = func(from, to string) *automa.WorkflowBuilder {
		return automa.NewWorkflowBuilder().WithId("hop-" + from + "-" + to).Steps(noop())
	}
}

func stateFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func readStateFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)
	return string(data)
}

func TestApplyCiliumHop_UpgradesTheCLIThenCiliumAndRecords(t *testing.T) {
	stubCiliumApply(t, false)
	src, dir := liveSources(t, v035Cluster())
	r := &fakeRunner{}

	require.NoError(t, ApplyCiliumHop(context.Background(), src, r.run, ciliumPatch()))

	assert.Equal(t, []string{"cli-0.20.1", "hop-1.18.1-1.18.16"}, r.ran)
	assert.Contains(t, readStateFile(t, dir, "system.yaml"), "version: 0.20.1")
	assert.Contains(t, readStateFile(t, dir, "cluster.yaml"), "cilium:")
	history := readStateFile(t, dir, "action_history.yaml")
	assert.Contains(t, history, "cilium-cli")
	assert.Contains(t, history, "to: 1.18.16")
}

func TestApplyCiliumHop_CLIAlreadyCurrent(t *testing.T) {
	stubCiliumApply(t, true)
	src, dir := liveSources(t, v035Cluster())
	r := &fakeRunner{}

	require.NoError(t, ApplyCiliumHop(context.Background(), src, r.run, ciliumPatch()))

	assert.Equal(t, []string{"hop-1.18.1-1.18.16"}, r.ran)
	assert.NotContains(t, stateFiles(t, dir), "system.yaml")
}

func TestApplyCiliumHop_DoneHopDoesNothing(t *testing.T) {
	stubCiliumApply(t, false)
	cs := v035Cluster()
	cs.CiliumAgentVersion = "1.18.16"
	cs.Releases["cilium"] = deployed("cilium", "1.18.16")
	src, dir := liveSources(t, cs)
	r := &fakeRunner{}

	require.NoError(t, ApplyCiliumHop(context.Background(), src, r.run, ciliumPatch()))

	assert.Empty(t, r.ran)
	assert.Empty(t, stateFiles(t, dir))
}

func TestApplyCiliumHop_FailedCheckChangesNothing(t *testing.T) {
	stubCiliumApply(t, false)
	src, dir := liveSources(t, v035Cluster())
	r := &fakeRunner{}

	err := ApplyCiliumHop(context.Background(), src, r.run, ciliumHop(StepMinor, "1.18.1", "1.20.2"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "one step at a time")
	assert.Empty(t, r.ran)
	assert.Empty(t, stateFiles(t, dir))
}

func TestApplyCiliumHop_FailedHopRecordsTheFailure(t *testing.T) {
	stubCiliumApply(t, true)
	src, dir := liveSources(t, v035Cluster())
	r := &fakeRunner{err: errorx.ExternalError.New("cilium upgrade failed")}

	err := ApplyCiliumHop(context.Background(), src, r.run, ciliumPatch())

	require.Error(t, err)
	assert.NotContains(t, stateFiles(t, dir), "cluster.yaml")
	assert.Contains(t, readStateFile(t, dir, "action_history.yaml"), "outcome: failed")
}

func TestApplyCiliumHop_FailsFastOnEachLock(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cliAt bool
		lock  state.ComponentID
	}{
		{"system, for the CLI", false, state.ComponentSystem},
		{"cluster, for the hop", true, state.ComponentCluster},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubCiliumApply(t, tc.cliAt)
			src, dir := liveSources(t, v035Cluster())
			release, err := state.AcquireComponentLocks(dir, []state.ComponentID{tc.lock}, 0)
			require.NoError(t, err)
			defer release()
			r := &fakeRunner{}

			require.Error(t, ApplyCiliumHop(context.Background(), src, r.run, ciliumPatch()))

			assert.Empty(t, r.ran, "no workflow may run without the lock")
			entries, err := state.ReadActionHistory(dir)
			require.NoError(t, err)
			assert.Empty(t, entries)
		})
	}
}

func TestApplyCiliumHop_NeedsARunner(t *testing.T) {
	src, _ := liveSources(t, v035Cluster())
	err := ApplyCiliumHop(context.Background(), src, nil, ciliumPatch())
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.IllegalArgument))
}
