// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/software"
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

// fakeRunner runs each hop's workflow as the command does, without rendering
// it, and keeps the reports.
type fakeRunner struct {
	reports []*automa.Report
}

func (f *fakeRunner) run(_ context.Context, fn func() (*automa.Report, error)) error {
	report, err := fn()
	if err != nil {
		return err
	}
	f.reports = append(f.reports, report)
	return steps.FailureOf(report)
}

// ran is the ids of the workflows run.
func (f *fakeRunner) ran() []string {
	var ids []string
	for _, r := range f.reports {
		ids = append(ids, r.Id)
	}
	return ids
}

// stepIDs is the ids of the steps of the only workflow run.
func (f *fakeRunner) stepIDs(t *testing.T) []string {
	t.Helper()
	require.Len(t, f.reports, 1)
	var ids []string
	for _, sr := range f.reports[0].StepReports {
		ids = append(ids, sr.Id)
	}
	return ids
}

func noopStep(id string, fail bool) automa.Builder {
	return automa.NewStepBuilder().WithId(id).WithExecute(func(_ context.Context, stp automa.Step) *automa.Report {
		if fail {
			return automa.StepFailureReport(stp.Id(), automa.WithError(errorx.ExternalError.New("%s failed", id)))
		}
		return automa.SuccessReport(stp)
	})
}

// stubHopFinish replaces the node check, which needs a cluster, and shortens
// the wait for the cluster to be readable.
func stubHopFinish(t *testing.T) {
	t.Helper()
	origStep, origTimeout, origInterval := nodesReadyStep, readableTimeout, readableInterval
	t.Cleanup(func() { nodesReadyStep, readableTimeout, readableInterval = origStep, origTimeout, origInterval })
	nodesReadyStep = func() automa.Builder { return noopStep(NodesReadyStepID, false) }
	readableTimeout, readableInterval = 50*time.Millisecond, time.Millisecond
}

// stubCiliumApply stubs the cilium CLI check and the hop workflow. The CLI is
// at the catalog default when cliAt is set, or once the workflow's CLI step
// has run; the hop's step fails when fail is set. It returns the cilium CLI
// version each built workflow was asked to run with.
func stubCiliumApply(t *testing.T, cliAt, fail bool) *[]string {
	t.Helper()
	stubHopFinish(t)
	origAt, origHop := hostBinaryAt, ciliumHopWorkflow
	t.Cleanup(func() { hostBinaryAt, ciliumHopWorkflow = origAt, origHop })
	installed := cliAt
	hostBinaryAt = func(string, string) (bool, error) { return installed, nil }
	var clis []string
	ciliumHopWorkflow = func(from, to, cli string) *automa.WorkflowBuilder {
		clis = append(clis, cli)
		return automa.NewWorkflowBuilder().WithId("hop-"+from+"-"+to).Steps(
			automa.NewStepBuilder().WithId("upgrade-cilium").
				WithExecute(func(_ context.Context, stp automa.Step) *automa.Report {
					installed = true
					return automa.SuccessReport(stp)
				}),
			noopStep("cilium-upgrade", fail))
	}
	return &clis
}

// holdLocks holds the system and cluster locks in dir for the test.
func holdLocks(t *testing.T, dir string) *state.HeldLocks {
	t.Helper()
	locks, err := state.AcquireHeldLocks(dir, []state.ComponentID{state.ComponentSystem, state.ComponentCluster}, 0)
	require.NoError(t, err)
	t.Cleanup(locks.Release)
	return locks
}

func historyEntries(t *testing.T, dir string) []state.ActionHistory {
	t.Helper()
	entries, err := state.ReadActionHistory(dir)
	require.NoError(t, err)
	return entries
}

func stateFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	return names
}

func readStateFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)
	return string(data)
}

// failingPersist is a state manager whose Persist fails.
type failingPersist struct{ state.Manager }

func (f failingPersist) Set(s state.State) state.Writer {
	f.Manager.Set(s)
	return f
}

func (failingPersist) Persist(...state.ComponentID) ([]state.ChangedFile, error) {
	return nil, errors.New("disk full")
}

// settlingChecker returns first on the first read and then on every later one.
type settlingChecker struct {
	first, then state.ClusterState
	calls       int
}

func (c *settlingChecker) RefreshState(context.Context) (state.ClusterState, error) {
	c.calls++
	if c.calls == 1 {
		return c.first, nil
	}
	return c.then, nil
}

func TestApplyCiliumHop_UpgradesTheCLIThenCiliumAndRecords(t *testing.T) {
	clis := stubCiliumApply(t, false, false)
	src, dir := liveSources(t, v035Cluster())
	r := &fakeRunner{}

	require.NoError(t, ApplyCiliumHop(context.Background(), src, holdLocks(t, dir), r.run, ciliumPatch()))

	assert.Equal(t, []string{"hop-1.18.1-1.18.16"}, r.ran())
	assert.Equal(t, []string{"0.20.1"}, *clis)
	assert.Equal(t, []string{"upgrade-cilium", "cilium-upgrade", NodesReadyStepID, steps.WaitClusterReadableStepID,
		steps.RecordStateStepID}, r.stepIDs(t))
	assert.Contains(t, readStateFile(t, dir, "system.yaml"), "version: 0.20.1")
	assert.Contains(t, readStateFile(t, dir, "cluster.yaml"), "cilium:")
	entries := historyEntries(t, dir)
	require.Len(t, entries, 1, "the CLI and the hop are one entry")
	assert.Equal(t, state.OutcomeSucceeded, entries[0].Outcome)
	assert.ElementsMatch(t, []state.ChangedFile{state.ComponentSystem.File(), state.ComponentCluster.File()}, entries[0].Components)
	history := readStateFile(t, dir, "action_history.yaml")
	assert.Contains(t, history, "cilium-cli: 0.20.1")
	assert.Contains(t, history, "to: 1.18.16")
}

// seedCLIRecord records the cilium CLI at version in src's state directory.
func seedCLIRecord(t *testing.T, src LiveSources, version string) {
	t.Helper()
	st := src.State.State()
	recordSoftware(&st, software.CiliumBinaryName, version)
	_, err := src.State.Set(st).Persist(state.ComponentSystem)
	require.NoError(t, err)
}

func TestApplyCiliumHop_CurrentCLIRecordIsLeftAsIs(t *testing.T) {
	clis := stubCiliumApply(t, true, false)
	src, dir := liveSources(t, v035Cluster())
	seedCLIRecord(t, src, "0.20.1")
	r := &fakeRunner{}

	require.NoError(t, ApplyCiliumHop(context.Background(), src, holdLocks(t, dir), r.run, ciliumPatch()))

	assert.Equal(t, []string{"0.20.1"}, *clis, "the hop runs with the catalog's CLI")
	entries := historyEntries(t, dir)
	require.Len(t, entries, 1)
	assert.Equal(t, []state.ChangedFile{state.ComponentCluster.File()}, entries[0].Components)
}

// An earlier run may have replaced the CLI without recording it.
func TestApplyCiliumHop_StaleCLIRecordIsRepaired(t *testing.T) {
	stubCiliumApply(t, true, false)
	src, dir := liveSources(t, v035Cluster())
	seedCLIRecord(t, src, "0.18.7")
	r := &fakeRunner{}

	require.NoError(t, ApplyCiliumHop(context.Background(), src, holdLocks(t, dir), r.run, ciliumPatch()))

	assert.Contains(t, readStateFile(t, dir, "system.yaml"), "version: 0.20.1")
	entries := historyEntries(t, dir)
	require.Len(t, entries, 1)
	assert.ElementsMatch(t, []state.ChangedFile{state.ComponentSystem.File(), state.ComponentCluster.File()}, entries[0].Components)
}

func TestApplyCiliumHop_DoneHopDoesNothing(t *testing.T) {
	stubCiliumApply(t, false, false)
	cs := v035Cluster()
	cs.CiliumAgentVersion = "1.18.16"
	cs.Releases["cilium"] = deployed("cilium", "1.18.16")
	src, dir := liveSources(t, cs)
	r := &fakeRunner{}

	require.NoError(t, ApplyCiliumHop(context.Background(), src, holdLocks(t, dir), r.run, ciliumPatch()))

	assert.Empty(t, r.ran())
	assert.Empty(t, stateFiles(t, dir))
}

func TestApplyCiliumHop_FailedCheckChangesNothing(t *testing.T) {
	stubCiliumApply(t, false, false)
	src, dir := liveSources(t, v035Cluster())
	r := &fakeRunner{}

	err := ApplyCiliumHop(context.Background(), src, holdLocks(t, dir), r.run, ciliumHop(StepMinor, "1.18.1", "1.20.2"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "one step at a time")
	assert.Empty(t, r.ran())
	assert.Empty(t, stateFiles(t, dir))
}

func TestApplyCiliumHop_FailedHopKeepsTheClusterRecord(t *testing.T) {
	stubCiliumApply(t, true, true)
	src, dir := liveSources(t, v035Cluster())
	r := &fakeRunner{}

	err := ApplyCiliumHop(context.Background(), src, holdLocks(t, dir), r.run, ciliumPatch())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cilium-upgrade failed")
	assert.NotContains(t, stateFiles(t, dir), "cluster.yaml")
	entries := historyEntries(t, dir)
	require.Len(t, entries, 1)
	assert.Equal(t, state.OutcomeFailed, entries[0].Outcome)
}

// A hop that fails after it replaced the cilium CLI records the CLI, so other
// commands verify the binary against the version now on disk.
func TestApplyCiliumHop_FailedHopRecordsTheCLIItReplaced(t *testing.T) {
	stubCiliumApply(t, false, true)
	src, dir := liveSources(t, v035Cluster())
	r := &fakeRunner{}

	err := ApplyCiliumHop(context.Background(), src, holdLocks(t, dir), r.run, ciliumPatch())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cilium-upgrade failed")
	assert.Contains(t, readStateFile(t, dir, "system.yaml"), "version: 0.20.1")
	assert.NotContains(t, stateFiles(t, dir), "cluster.yaml")
	entries := historyEntries(t, dir)
	require.Len(t, entries, 1, "the failure and what it recorded are one entry")
	assert.Equal(t, state.OutcomeFailed, entries[0].Outcome)
	assert.Equal(t, []state.ChangedFile{state.ComponentSystem.File()}, entries[0].Components)
	assert.False(t, entries[0].NotPersisted)
}

func TestApplyCiliumHop_FailedCLIUpgradeKeepsItsRecord(t *testing.T) {
	stubCiliumApply(t, false, false)
	ciliumHopWorkflow = func(string, string, string) *automa.WorkflowBuilder {
		return automa.NewWorkflowBuilder().WithId("hop").Steps(noopStep("upgrade-cilium", true))
	}
	src, dir := liveSources(t, v035Cluster())
	r := &fakeRunner{}

	err := ApplyCiliumHop(context.Background(), src, holdLocks(t, dir), r.run, ciliumPatch())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "upgrade-cilium failed")
	assert.NotContains(t, stateFiles(t, dir), "system.yaml")
	entries := historyEntries(t, dir)
	require.Len(t, entries, 1)
	assert.Equal(t, state.OutcomeFailed, entries[0].Outcome)
	assert.Empty(t, entries[0].Components)
}

// What a failed hop changed that cannot be recorded is a warning: the hop's
// own failure is the error.
func TestApplyCiliumHop_UnrecordedCLIAfterAFailureIsAWarning(t *testing.T) {
	stubCiliumApply(t, false, true)
	src, dir := liveSources(t, v035Cluster())
	src.State = failingPersist{src.State}
	r := &fakeRunner{}

	err := ApplyCiliumHop(context.Background(), src, holdLocks(t, dir), r.run, ciliumPatch())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cilium-upgrade failed")
	require.Len(t, r.reports, 1)
	assert.Contains(t, r.reports[0].Metadata[models.ReportMetaWarning], "What the failed hop changed was not recorded: disk full")
	entries := historyEntries(t, dir)
	require.Len(t, entries, 1)
	assert.Equal(t, state.OutcomeFailed, entries[0].Outcome)
	assert.True(t, entries[0].NotPersisted)
}

// A record that cannot be written does not fail a hop that ran: the run gets a
// warning and its entry is marked NotPersisted.
func TestApplyCiliumHop_RecordFailureIsAWarning(t *testing.T) {
	stubCiliumApply(t, true, false)
	src, dir := liveSources(t, v035Cluster())
	src.State = failingPersist{src.State}
	r := &fakeRunner{}

	require.NoError(t, ApplyCiliumHop(context.Background(), src, holdLocks(t, dir), r.run, ciliumPatch()))

	require.Len(t, r.reports, 1)
	assert.True(t, r.reports[0].IsSuccess())
	var warnings []string
	for _, sr := range r.reports[0].StepReports {
		warnings = append(warnings, sr.Metadata[models.ReportMetaWarning])
	}
	assert.Contains(t, strings.Join(warnings, "\n"), "The state was not recorded: disk full")
	entries := historyEntries(t, dir)
	require.Len(t, entries, 1)
	assert.Equal(t, state.OutcomeSucceeded, entries[0].Outcome)
	assert.True(t, entries[0].NotPersisted)
}

// A cluster that cannot be fully read after the hop fails it, so it is never
// recorded from a cluster that is still settling.
func TestApplyCiliumHop_UnsettledClusterFailsTheHop(t *testing.T) {
	stubCiliumApply(t, true, false)
	unread := v035Cluster()
	unread.Unobserved = true
	src, dir := liveSources(t, v035Cluster())
	src.Cluster = &settlingChecker{first: v035Cluster(), then: unread}
	r := &fakeRunner{}

	err := ApplyCiliumHop(context.Background(), src, holdLocks(t, dir), r.run, ciliumPatch())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not be fully read")
	assert.NotContains(t, stateFiles(t, dir), "cluster.yaml")
	entries := historyEntries(t, dir)
	require.Len(t, entries, 1)
	assert.Equal(t, state.OutcomeFailed, entries[0].Outcome)
}

func TestApplyCiliumHop_RefusesLocksThatDoNotCoverIt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		locks func(t *testing.T, dir string) *state.HeldLocks
	}{
		{"no locks", func(*testing.T, string) *state.HeldLocks { return nil }},
		{"only the cluster lock", func(t *testing.T, dir string) *state.HeldLocks {
			l, err := state.AcquireHeldLocks(dir, []state.ComponentID{state.ComponentCluster}, 0)
			require.NoError(t, err)
			t.Cleanup(l.Release)
			return l
		}},
		{"locks in another directory", func(t *testing.T, _ string) *state.HeldLocks { return holdLocks(t, t.TempDir()) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubCiliumApply(t, false, false)
			src, dir := liveSources(t, v035Cluster())
			r := &fakeRunner{}

			err := ApplyCiliumHop(context.Background(), src, tc.locks(t, dir), r.run, ciliumPatch())

			require.Error(t, err)
			reason, _ := errx.ReasonOf(err)
			assert.Equal(t, reasons.Internal, reason)
			assert.Empty(t, r.ran(), "no workflow may run without the locks")
			assert.Empty(t, historyEntries(t, dir))
		})
	}
}

func TestApplyCiliumHop_UnreachableClusterIsRefused(t *testing.T) {
	stubCiliumApply(t, false, false)
	src, dir := liveSources(t, v035Cluster())
	src.Probe = probeReturns(reality.NotConfigured)
	r := &fakeRunner{}

	err := ApplyCiliumHop(context.Background(), src, holdLocks(t, dir), r.run, ciliumPatch())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no kubeconfig")
	reason, _ := errx.ReasonOf(err)
	assert.Equal(t, reasons.PreconditionNotMet, reason)
	hints, _ := errx.Hints(err)
	assert.Contains(t, strings.Join(hints, " "), "sudo")
	assert.Empty(t, r.ran())
	assert.Empty(t, historyEntries(t, dir))
}

func TestApplyCiliumHop_NeedsARunner(t *testing.T) {
	src, dir := liveSources(t, v035Cluster())
	err := ApplyCiliumHop(context.Background(), src, holdLocks(t, dir), nil, ciliumPatch())
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.IllegalArgument))
}
