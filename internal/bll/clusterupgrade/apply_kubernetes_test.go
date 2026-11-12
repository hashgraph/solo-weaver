// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	k8sversion "k8s.io/apimachinery/pkg/version"

	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
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

func stubKubernetesApply(t *testing.T, found onDisk) {
	t.Helper()
	orig, origIdentify := kubernetesHopWorkflow, identifyHostVersions
	t.Cleanup(func() { kubernetesHopWorkflow, identifyHostVersions = orig, origIdentify })
	identifyHostVersions = func(artifact string) ([]string, error) { return found[artifact], nil }
	kubernetesHopWorkflow = func(from, to, crio string) *automa.WorkflowBuilder {
		return automa.NewWorkflowBuilder().WithId("kube-" + from + "-" + to + "-" + crio).
			Steps(automa.NewStepBuilder().WithId("noop").WithExecute(func(_ context.Context, stp automa.Step) *automa.Report {
				return automa.SuccessReport(stp)
			}))
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
	stubKubernetesApply(t, at("1.33.13", "1.33.13"))
	src, dir := liveSources(t, v035Node())
	r := &fakeRunner{}

	require.NoError(t, ApplyKubernetesHop(context.Background(), src, r.run, kubePatch()))

	assert.Equal(t, []string{"kube-1.33.4-1.33.13-1.33.13"}, r.ran)
	sw := recordedSoftware(t, dir)
	for _, name := range []string{"kubeadm", "kubelet", "kubectl"} {
		assert.Equal(t, map[string]any{"name": name, "version": "1.33.13", "installed": true}, pick(sw[name], "name", "version", "installed"), name)
	}
	assert.Equal(t, map[string]any{"name": "cri-o", "version": "1.33.13", "installed": true}, pick(sw["crio"], "name", "version", "installed"))
	assert.Contains(t, readStateFile(t, dir, "cluster.yaml"), "kubeletVersion")
	history := readStateFile(t, dir, "action_history.yaml")
	assert.Contains(t, history, "component: kubernetes")
	assert.Contains(t, history, "to: 1.33.13")
}

func TestApplyKubernetesHop_DoneHopDoesNothing(t *testing.T) {
	stubKubernetesApply(t, at("1.33.13", "1.33.13"))
	cs := v035Node()
	cs.ServerVersion = k8sversion.Info{GitVersion: "v1.33.13"}
	cs.KubeletVersion = "1.33.13"
	cs.ContainerRuntimeVersion = "cri-o://1.33.13"
	src, dir := liveSources(t, cs)
	r := &fakeRunner{}

	require.NoError(t, ApplyKubernetesHop(context.Background(), src, r.run, kubePatch()))

	assert.Empty(t, r.ran)
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
			stubKubernetesApply(t, at("1.33.13", "1.33.13"))
			src, dir := liveSources(t, v035Node())
			r := &fakeRunner{}

			err := ApplyKubernetesHop(context.Background(), src, r.run, tc.hop)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.check)
			assert.Empty(t, r.ran)
			assert.Empty(t, stateFiles(t, dir))
		})
	}
}

func TestApplyKubernetesHop_FailedHopRecordsTheBinariesItReplaced(t *testing.T) {
	found := at("1.33.4", "1.33.4")
	found["kubeadm"] = []string{"1.33.13"} // replaced before the apply failed
	stubKubernetesApply(t, found)
	src, dir := liveSources(t, v035Node())
	r := &fakeRunner{err: errorx.ExternalError.New("kubeadm upgrade apply failed")}

	err := ApplyKubernetesHop(context.Background(), src, r.run, kubePatch())

	require.Error(t, err)
	assert.NotContains(t, stateFiles(t, dir), "cluster.yaml")
	sw := recordedSoftware(t, dir)
	assert.Equal(t, "1.33.13", sw["kubeadm"]["version"])
	assert.Equal(t, "1.33.4", sw["kubelet"]["version"])
	assert.Equal(t, "1.33.4", sw["crio"]["version"])
	entries, err := state.ReadActionHistory(dir)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, state.OutcomeFailed, entries[0].Outcome)
	assert.Equal(t, state.OutcomeSucceeded, entries[1].Outcome)
	assert.Contains(t, readStateFile(t, dir, "action_history.yaml"), "host binaries after a failed hop")
}

func TestApplyKubernetesHop_UnidentifiedBinaryKeepsItsRecord(t *testing.T) {
	found := at("1.33.13", "1.33.13")
	found["kubectl"] = nil // matches no catalog version
	stubKubernetesApply(t, found)
	src, dir := liveSources(t, v035Node())
	r := &fakeRunner{}

	require.NoError(t, ApplyKubernetesHop(context.Background(), src, r.run, kubePatch()))

	sw := recordedSoftware(t, dir)
	assert.Equal(t, "1.33.13", sw["kubelet"]["version"])
	assert.NotContains(t, sw, "kubectl")
}

func TestApplyKubernetesHop_FailsFastOnEachLock(t *testing.T) {
	for _, id := range []state.ComponentID{state.ComponentSystem, state.ComponentCluster} {
		t.Run(string(id), func(t *testing.T) {
			stubKubernetesApply(t, at("1.33.13", "1.33.13"))
			src, dir := liveSources(t, v035Node())
			release, err := state.AcquireComponentLocks(dir, []state.ComponentID{id}, 0)
			require.NoError(t, err)
			defer release()
			r := &fakeRunner{}

			require.Error(t, ApplyKubernetesHop(context.Background(), src, r.run, kubePatch()))

			assert.Empty(t, r.ran, "no workflow may run without the lock")
			entries, err := state.ReadActionHistory(dir)
			require.NoError(t, err)
			assert.Empty(t, entries)
		})
	}
}

func TestApplyKubernetesHop_NeedsARunner(t *testing.T) {
	src, _ := liveSources(t, v035Node())
	err := ApplyKubernetesHop(context.Background(), src, nil, kubePatch())
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.IllegalArgument))
}
