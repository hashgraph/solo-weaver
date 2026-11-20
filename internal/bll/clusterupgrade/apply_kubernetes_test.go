// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"context"
	"strings"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	k8sversion "k8s.io/apimachinery/pkg/version"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

func withKubelet(v string) shapeOpt {
	return func(s *rsl.ClusterShape) { s.Kubernetes.Kubelet = v }
}

func TestKubernetesHopChecks_Pass(t *testing.T) {
	for _, tc := range []struct {
		name  string
		shape rsl.ClusterShape
		hop   Hop
	}{
		{"patch", clusterShape("1.33.4", "1.18.16"), kubeHop(StepPatch, "1.33.4", "1.33.13")},
		{"minor", clusterShape("1.33.13", "1.19.10"), kubeHop(StepMinor, "1.33.13", "1.34.13")},
		{"minor with the charts it needs", clusterShape("1.35.10", "1.20.2", withChart("metallb", "0.16.1")), kubeHop(StepMinor, "1.35.10", "1.36.5")},
		{"resume", clusterShape("1.35.10", "1.20.2", withKubelet("1.34.13")), kubeHop(StepResume, "1.34.13", "1.35.10")},
		{"re-run after the control plane moved", clusterShape("1.34.13", "1.19.10", withKubelet("1.33.13")), kubeHop(StepMinor, "1.33.13", "1.34.13")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checks, err := KubernetesHopChecks(tc.shape, fixtureMatrix(), tc.hop)
			require.NoError(t, err)
			assert.Empty(t, checks.Failed())
			assert.NoError(t, checks.Err())
			assert.Len(t, checks, 7)
		})
	}
}

func TestKubernetesHopChecks_Fail(t *testing.T) {
	unread := clusterShape("1.33.4", "1.18.16")
	unread.Observed = false
	nodes := clusterShape("1.33.4", "1.18.16")
	nodes.Kubernetes.NodeCount, nodes.Kubernetes.UnexpectedNodes = 3, true
	legacy := clusterShape("1.35.10", "1.20.2", withLegacy("metallb", "0.16.1"))

	for _, tc := range []struct {
		name  string
		shape rsl.ClusterShape
		hop   Hop
		check string
	}{
		{"cluster not read", unread, kubeHop(StepPatch, "1.33.4", "1.33.13"), "the cluster was read"},
		{"more nodes", nodes, kubeHop(StepPatch, "1.33.4", "1.33.13"), "one node"},
		{"control plane elsewhere", clusterShape("1.33.4", "1.18.16"), kubeHop(StepPatch, "1.33.0", "1.33.13"), "Kubernetes is where the hop starts"},
		{"kubelet ahead", clusterShape("1.33.4", "1.18.16", withKubelet("1.33.13")), kubeHop(StepPatch, "1.33.4", "1.33.13"), "Kubernetes is where the hop starts"},
		{"resume before the control plane moved", clusterShape("1.34.13", "1.19.10"), kubeHop(StepResume, "1.34.13", "1.35.10"), "Kubernetes is where the hop starts"},
		{"target not shipped", clusterShape("1.33.4", "1.18.16"), kubeHop(StepPatch, "1.33.4", "1.33.12"), "the target is a catalog version"},
		{"two minors", clusterShape("1.33.13", "1.20.2"), kubeHop(StepMinor, "1.33.13", "1.35.10"), "one step at a time"},
		{"patch across minors", clusterShape("1.33.13", "1.19.10"), kubeHop(StepPatch, "1.33.13", "1.34.13"), "one step at a time"},
		{"downgrade", clusterShape("1.33.13", "1.18.16"), kubeHop(StepPatch, "1.33.13", "1.33.4"), "one step at a time"},
		{"resume two minors", clusterShape("1.35.10", "1.20.2", withKubelet("1.33.13")), kubeHop(StepResume, "1.33.13", "1.35.10"), "one step at a time"},
		{"Cilium too old", clusterShape("1.33.13", "1.18.16"), kubeHop(StepMinor, "1.33.13", "1.34.13"), "the running Cilium supports the target"},
		{"Cilium unknown", clusterShape("1.33.13", ""), kubeHop(StepMinor, "1.33.13", "1.34.13"), "the running Cilium supports the target"},
		{"chart too old", clusterShape("1.35.10", "1.20.2", withChart("metallb", "0.15.2")), kubeHop(StepMinor, "1.35.10", "1.36.5"), "the charts the target needs are installed"},
		{"chart under a legacy identity", legacy, kubeHop(StepMinor, "1.35.10", "1.36.5"), "the charts the target needs are installed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checks, err := KubernetesHopChecks(tc.shape, fixtureMatrix(), tc.hop)
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

func TestKubernetesHopChecks_RejectsOtherHops(t *testing.T) {
	for _, hop := range []Hop{
		ciliumHop(StepPatch, "1.18.1", "1.18.16"),
		{Kind: KindKubernetes, Step: StepCoupled, From: "1.33.4", To: "1.33.13"},
		kubeHop(StepPatch, "1.33", "1.33.13"),
		kubeHop(StepPatch, "1.33.4", "latest"),
	} {
		_, err := KubernetesHopChecks(clusterShape("1.33.4", "1.18.16"), fixtureMatrix(), hop)
		require.Error(t, err)
		assert.True(t, errorx.IsOfType(err, errorx.IllegalArgument), err.Error())
		reason, _ := errx.ReasonOf(err)
		assert.Equal(t, reasons.Internal, reason)
	}
}

func TestKubernetesHopChecks_InvalidMatrix(t *testing.T) {
	_, err := KubernetesHopChecks(clusterShape("1.33.4", "1.18.16"), Matrix{}, kubeHop(StepPatch, "1.33.4", "1.33.13"))
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.IllegalFormat), err.Error())
}

func TestCrioFor(t *testing.T) {
	cat, err := software.LoadInfrastructureCatalog()
	require.NoError(t, err)
	for kube, crio := range map[string]string{
		"1.33.13": "1.33.13",
		"1.34.13": "1.34.15",
		"1.35.10": "1.35.11",
		"1.36.5":  "1.36.7",
	} {
		got, err := crioFor(cat, kube)
		require.NoError(t, err, kube)
		assert.Equal(t, crio, got, kube)
	}

	_, err = crioFor(cat, "1.37.0")
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.IllegalState), err.Error())
	_, err = crioFor(cat, "1.36")
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.IllegalArgument), err.Error())
}

// onDisk is what the stubbed hashing finds for each host artifact.
type onDisk map[string][]string

// stubKubernetesApply stubs the binary hashing and the hop workflow, whose
// step fails when fail is set.
func stubKubernetesApply(t *testing.T, found onDisk, fail bool) {
	t.Helper()
	stubHopFinish(t)
	orig, origIdentify := kubernetesHopWorkflow, identifyHostVersions
	t.Cleanup(func() { kubernetesHopWorkflow, identifyHostVersions = orig, origIdentify })
	identifyHostVersions = func(artifact string) ([]string, error) { return found[artifact], nil }
	kubernetesHopWorkflow = func(from, to, crio string) *automa.WorkflowBuilder {
		return automa.NewWorkflowBuilder().WithId("kube-" + from + "-" + to + "-" + crio).
			Steps(noopStep("kubernetes-upgrade", fail))
	}
}

func at(kube, crio string) onDisk {
	return onDisk{"kubeadm": {kube}, "kubelet": {kube}, "kubectl": {kube}, "cri-o": {crio}}
}

func v035Node() state.ClusterState {
	cs := v035Cluster()
	cs.ContainerRuntimeVersion = "cri-o://1.33.4"
	return cs
}

// recordedSoftware reads the software entries from system.yaml.
func recordedSoftware(t *testing.T, dir string) map[string]map[string]any {
	t.Helper()
	var doc any
	require.NoError(t, yaml.Unmarshal([]byte(readStateFile(t, dir, "system.yaml")), &doc))
	var find func(v any) map[string]any
	find = func(v any) map[string]any {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		if sw, ok := m["software"].(map[string]any); ok {
			return sw
		}
		for _, child := range m {
			if sw := find(child); sw != nil {
				return sw
			}
		}
		return nil
	}
	out := map[string]map[string]any{}
	for name, entry := range find(doc) {
		out[name], _ = entry.(map[string]any)
	}
	return out
}

func pick(m map[string]any, keys ...string) map[string]any {
	out := map[string]any{}
	for _, k := range keys {
		out[k] = m[k]
	}
	return out
}

func kubePatch() Hop { return kubeHop(StepPatch, "1.33.4", "1.33.13") }

func TestApplyKubernetesHop_RunsTheHopAndRecords(t *testing.T) {
	stubKubernetesApply(t, at("1.33.13", "1.33.13"), false)
	src, dir := liveSources(t, v035Node())
	r := &fakeRunner{}

	require.NoError(t, ApplyKubernetesHop(context.Background(), src, holdLocks(t, dir), r.run, kubePatch()))

	assert.Equal(t, []string{"kube-1.33.4-1.33.13-1.33.13"}, r.ran())
	assert.Equal(t, []string{"kubernetes-upgrade", NodesReadyStepID, steps.WaitClusterReadableStepID, steps.RecordStateStepID},
		r.stepIDs(t))
	sw := recordedSoftware(t, dir)
	for _, name := range []string{"kubeadm", "kubelet", "kubectl"} {
		assert.Equal(t, map[string]any{"name": name, "version": "1.33.13", "installed": true}, pick(sw[name], "name", "version", "installed"), name)
	}
	assert.Equal(t, map[string]any{"name": "cri-o", "version": "1.33.13", "installed": true}, pick(sw["crio"], "name", "version", "installed"))
	assert.Contains(t, readStateFile(t, dir, "cluster.yaml"), "kubeletVersion")
	entries := historyEntries(t, dir)
	require.Len(t, entries, 1)
	assert.Equal(t, state.OutcomeSucceeded, entries[0].Outcome)
	assert.ElementsMatch(t, []state.ChangedFile{state.ComponentSystem.File(), state.ComponentCluster.File()}, entries[0].Components)
	history := readStateFile(t, dir, "action_history.yaml")
	for _, want := range []string{"component: kubernetes", "step: patch", "from: 1.33.4", "to: 1.33.13", "cri-o: 1.33.13"} {
		assert.Contains(t, history, want)
	}
}

func TestApplyKubernetesHop_DoneHopDoesNothing(t *testing.T) {
	stubKubernetesApply(t, at("1.33.13", "1.33.13"), false)
	cs := v035Node()
	cs.ServerVersion = k8sversion.Info{GitVersion: "v1.33.13"}
	cs.KubeletVersion = "1.33.13"
	cs.ContainerRuntimeVersion = "cri-o://1.33.13"
	src, dir := liveSources(t, cs)
	r := &fakeRunner{}

	require.NoError(t, ApplyKubernetesHop(context.Background(), src, holdLocks(t, dir), r.run, kubePatch()))

	assert.Empty(t, r.ran())
	assert.Empty(t, stateFiles(t, dir))
}

func TestApplyKubernetesHop_FailedCheckChangesNothing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		hop   Hop
		check string
	}{
		{"Cilium too old", kubeHop(StepMinor, "1.33.4", "1.34.13"), "the running Cilium supports the target"},
		{"target not in the catalog", kubeHop(StepPatch, "1.33.4", "1.33.12"), "the target is a catalog version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubKubernetesApply(t, at("1.33.13", "1.33.13"), false)
			src, dir := liveSources(t, v035Node())
			r := &fakeRunner{}

			err := ApplyKubernetesHop(context.Background(), src, holdLocks(t, dir), r.run, tc.hop)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.check)
			reason, _ := errx.ReasonOf(err)
			assert.Equal(t, reasons.PreconditionNotMet, reason)
			assert.Empty(t, r.ran())
			assert.Empty(t, stateFiles(t, dir))
		})
	}
}

// A failed hop may have replaced host binaries; other commands verify a binary
// against its record, so the record must follow what is on disk.
func TestApplyKubernetesHop_FailedHopRecordsTheBinariesItReplaced(t *testing.T) {
	found := at("1.33.4", "1.33.4")
	found["kubeadm"] = []string{"1.33.13"} // replaced before the apply failed
	stubKubernetesApply(t, found, true)
	src, dir := liveSources(t, v035Node())
	r := &fakeRunner{}

	err := ApplyKubernetesHop(context.Background(), src, holdLocks(t, dir), r.run, kubePatch())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "kubernetes-upgrade failed")
	assert.NotContains(t, stateFiles(t, dir), "cluster.yaml")
	sw := recordedSoftware(t, dir)
	assert.Equal(t, "1.33.13", sw["kubeadm"]["version"])
	assert.Equal(t, "1.33.4", sw["kubelet"]["version"])
	assert.Equal(t, "1.33.4", sw["crio"]["version"])
	entries := historyEntries(t, dir)
	require.Len(t, entries, 1, "the failure and what it recorded are one entry")
	assert.Equal(t, state.OutcomeFailed, entries[0].Outcome)
	assert.Equal(t, []state.ChangedFile{state.ComponentSystem.File()}, entries[0].Components)
	assert.False(t, entries[0].NotPersisted)
}

func TestApplyKubernetesHop_UnrecordedBinariesAfterAFailureAreAWarning(t *testing.T) {
	stubKubernetesApply(t, at("1.33.13", "1.33.4"), true)
	src, dir := liveSources(t, v035Node())
	src.State = failingPersist{src.State}
	r := &fakeRunner{}

	err := ApplyKubernetesHop(context.Background(), src, holdLocks(t, dir), r.run, kubePatch())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "kubernetes-upgrade failed")
	require.Len(t, r.reports, 1)
	assert.Contains(t, r.reports[0].Metadata[models.ReportMetaWarning], "What the failed hop changed was not recorded: disk full")
	entries := historyEntries(t, dir)
	require.Len(t, entries, 1)
	assert.Equal(t, state.OutcomeFailed, entries[0].Outcome)
	assert.True(t, entries[0].NotPersisted)
}

func TestApplyKubernetesHop_UnidentifiedBinaryKeepsItsRecord(t *testing.T) {
	found := at("1.33.13", "1.33.13")
	found["kubectl"] = nil // matches no catalog version
	stubKubernetesApply(t, found, false)
	src, dir := liveSources(t, v035Node())
	r := &fakeRunner{}

	require.NoError(t, ApplyKubernetesHop(context.Background(), src, holdLocks(t, dir), r.run, kubePatch()))

	sw := recordedSoftware(t, dir)
	assert.Equal(t, "1.33.13", sw["kubelet"]["version"])
	assert.NotContains(t, sw, "kubectl")
}

// A cluster that cannot be fully read after the hop fails it, so it is never
// recorded from a cluster that is still settling.
func TestApplyKubernetesHop_UnsettledClusterFailsTheHop(t *testing.T) {
	stubKubernetesApply(t, at("1.33.13", "1.33.13"), false)
	unread := v035Node()
	unread.Unobserved = true
	src, dir := liveSources(t, v035Node())
	src.Cluster = &settlingChecker{first: v035Node(), then: unread}
	r := &fakeRunner{}

	err := ApplyKubernetesHop(context.Background(), src, holdLocks(t, dir), r.run, kubePatch())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not be fully read")
	assert.NotContains(t, stateFiles(t, dir), "cluster.yaml")
	entries := historyEntries(t, dir)
	require.Len(t, entries, 1)
	assert.Equal(t, state.OutcomeFailed, entries[0].Outcome)
}

func TestApplyKubernetesHop_RefusesLocksThatDoNotCoverIt(t *testing.T) {
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
			stubKubernetesApply(t, at("1.33.13", "1.33.13"), false)
			src, dir := liveSources(t, v035Node())
			r := &fakeRunner{}

			err := ApplyKubernetesHop(context.Background(), src, tc.locks(t, dir), r.run, kubePatch())

			require.Error(t, err)
			reason, _ := errx.ReasonOf(err)
			assert.Equal(t, reasons.Internal, reason)
			assert.Empty(t, r.ran(), "no workflow may run without the locks")
			assert.Empty(t, historyEntries(t, dir))
		})
	}
}

func TestApplyKubernetesHop_UnreachableClusterIsRefused(t *testing.T) {
	stubKubernetesApply(t, at("1.33.13", "1.33.13"), false)
	src, dir := liveSources(t, v035Node())
	src.Probe = probeReturns(reality.Unobservable)
	r := &fakeRunner{}

	err := ApplyKubernetesHop(context.Background(), src, holdLocks(t, dir), r.run, kubePatch())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not answer")
	reason, _ := errx.ReasonOf(err)
	assert.Equal(t, reasons.PreconditionNotMet, reason)
	hints, _ := errx.Hints(err)
	assert.Contains(t, strings.Join(hints, " "), "readyz")
	assert.Empty(t, r.ran())
	assert.Empty(t, historyEntries(t, dir))
}

func TestApplyKubernetesHop_NeedsARunner(t *testing.T) {
	src, dir := liveSources(t, v035Node())
	err := ApplyKubernetesHop(context.Background(), src, holdLocks(t, dir), nil, kubePatch())
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.IllegalArgument))
}
