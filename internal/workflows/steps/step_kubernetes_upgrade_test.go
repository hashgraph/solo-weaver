// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/automa-saga/automa"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// fakeNode is a single-node cluster that reacts to the hop's actions: the
// apply moves the control plane, the installers move the binaries, and a
// kubelet (re)start brings the node up on the installed binaries.
type fakeNode struct {
	to            string
	health        kubeHealth
	binaries      map[string]string
	events        []string
	registry      string
	failOn        map[string]error // event -> error returned once
	kubeletStalls bool             // the kubelet never comes back Ready
	settling      int              // reads during which the scheduler is not Ready
}

func healthyNode(kubeVersion, crio string) kubeHealth {
	return kubeHealth{
		APIServer: kubeVersion, Nodes: 1, Node: "node1", Ready: true,
		Kubelet: kubeVersion, Runtime: "cri-o://" + crio,
		ControlPlane: map[string]staticPod{
			"kube-apiserver":          {Version: kubeVersion, Ready: true},
			"kube-controller-manager": {Version: kubeVersion, Ready: true},
			"kube-scheduler":          {Version: kubeVersion, Ready: true},
			"etcd":                    {Version: "3.5.21-0", Ready: true},
		},
	}
}

func (f *fakeNode) event(e string) error {
	f.events = append(f.events, e)
	if err, ok := f.failOn[e]; ok {
		delete(f.failOn, e)
		return err
	}
	return nil
}

func (f *fakeNode) kubeletUp() {
	if f.kubeletStalls {
		return
	}
	f.health.Ready = true
	f.health.Kubelet = f.binaries[software.KubeletBinaryName]
	f.health.Runtime = "cri-o://" + f.binaries[software.CrioArtifactName]
}

func newFakeNode(t *testing.T, kubeVersion, crio, to string) *fakeNode {
	t.Helper()
	f := &fakeNode{
		to:     to,
		health: healthyNode(kubeVersion, crio),
		binaries: map[string]string{
			software.KubeadmBinaryName: kubeVersion, software.KubeletBinaryName: kubeVersion,
			software.KubectlBinaryName: kubeVersion, software.CrioArtifactName: crio,
		},
		failOn: map[string]error{},
	}

	origHealth, origCordon, origStop, origStart, origRestart, origReload := readKubeHealth, setNodeCordon, stopService, startService, restartService, daemonReload
	origRemove, origRegistries, origPoll, origTimeout, origInstallers, origRegistry := removeCrioRelease, installRegistries, kubePollInterval, kubeHealthTimeout, kubeHopInstallers, containerRegistryOf
	origStartTimeout := kubeStartTimeout
	origIdentify, origScript, origVerify := identifyInstalledVersions, runBashScript, verifyExecutables
	t.Cleanup(func() {
		readKubeHealth, setNodeCordon, stopService, startService, restartService, daemonReload = origHealth, origCordon, origStop, origStart, origRestart, origReload
		removeCrioRelease, installRegistries, kubePollInterval, kubeHealthTimeout, kubeHopInstallers, containerRegistryOf = origRemove, origRegistries, origPoll, origTimeout, origInstallers, origRegistry
		identifyInstalledVersions, runBashScript, verifyExecutables = origIdentify, origScript, origVerify
		kubeStartTimeout = origStartTimeout
	})

	readKubeHealth = func(context.Context, *kube.Client) (kubeHealth, error) {
		h := f.health
		h.ControlPlane = maps.Clone(f.health.ControlPlane)
		if f.settling > 0 {
			f.settling--
			p := h.ControlPlane["kube-scheduler"]
			p.Ready = false
			h.ControlPlane["kube-scheduler"] = p
		}
		return h, nil
	}
	setNodeCordon = func(_ context.Context, _ *kube.Client, node string, cordon bool) error {
		name := "uncordon " + node
		if cordon {
			name = "cordon " + node
		}
		if err := f.event(name); err != nil {
			return err
		}
		f.health.Unschedulable, f.health.OurCordon = cordon, cordon
		return nil
	}
	stopService = func(_ context.Context, svc string) error {
		if svc == software.KubeletServiceName {
			f.health.Ready = false
		}
		return f.event("stop " + svc)
	}
	startService = func(_ context.Context, svc string) error {
		if svc == software.KubeletServiceName {
			f.kubeletUp()
		}
		return f.event("start " + svc)
	}
	restartService = func(_ context.Context, svc string) error {
		if svc == software.KubeletServiceName {
			f.kubeletUp()
		}
		return f.event("restart " + svc)
	}
	daemonReload = func(context.Context) error { return f.event("daemon-reload") }
	removeCrioRelease = func() error { return f.event("remove cri-o release files") }
	installRegistries = func(reg string) error { return f.event("registries " + reg) }
	containerRegistryOf = func() string { return f.registry }
	kubePollInterval, kubeHealthTimeout, kubeStartTimeout = time.Millisecond, 50*time.Millisecond, 50*time.Millisecond

	identifyInstalledVersions = func(name string) ([]string, error) { return []string{f.binaries[name]}, nil }
	verifyExecutables = func(string) error { return nil }
	runBashScript = func(scripts []string, _ string) (string, error) {
		if err := f.event("run " + strings.Join(scripts, "; ")); err != nil {
			return "", err
		}
		f.health.APIServer = f.to
		for _, c := range controlPlaneComponents {
			f.health.ControlPlane[c] = staticPod{Version: f.to, Ready: true}
		}
		return "", nil
	}

	ctrl := gomock.NewController(t)
	kubeHopInstallers = func(kubeVersion, crioVersion string) []kubeHopArtifact {
		out := defaultKubeHopInstallers(kubeVersion, crioVersion)
		for i := range out {
			name, version := out[i].name, out[i].version
			out[i].provider = func(...software.InstallerOption) (software.Software, error) {
				m := software.NewMockSoftware(ctrl)
				for _, phase := range []struct {
					call *gomock.Call
					name string
				}{
					{m.EXPECT().Download(), "download"},
					{m.EXPECT().Extract(), "extract"},
					{m.EXPECT().Install(), "install"},
					{m.EXPECT().Configure(), "configure"},
				} {
					ev := phase.name + " " + name
					phase.call.DoAndReturn(func() error {
						if err := f.event(ev); err != nil {
							return err
						}
						if strings.HasPrefix(ev, "install ") {
							f.binaries[name] = version
						}
						return nil
					}).AnyTimes()
				}
				m.EXPECT().Cleanup().AnyTimes()
				return m, nil
			}
		}
		return out
	}
	return f
}

func kubeHopRun(t *testing.T, from, to, crio string) []*automa.Report {
	t.Helper()
	return runSteps(t, KubernetesHopSteps(from, to, crio, func() (*kube.Client, error) { return &kube.Client{}, nil }))
}

func requireAllSucceeded(t *testing.T, reports []*automa.Report) {
	t.Helper()
	require.Len(t, reports, 12)
	for _, r := range reports {
		require.NoError(t, r.Error, r.Id)
		assert.NotEqual(t, automa.StatusFailed, r.Status, r.Id)
	}
}

var nodeSwap = []string{
	"stop kubelet", "stop crio",
	"download cri-o", "extract cri-o", "remove cri-o release files", "install cri-o", "configure cri-o",
	"download kubelet", "install kubelet", "configure kubelet",
	"download kubectl", "install kubectl", "configure kubectl",
	"daemon-reload", "start crio", "restart kubelet",
}

func TestKubernetesHop_MinorHop(t *testing.T) {
	f := newFakeNode(t, "1.33.13", "1.33.13", "1.34.13")

	reports := kubeHopRun(t, "1.33.13", "1.34.13", "1.34.15")

	requireAllSucceeded(t, reports)
	want := []string{
		"download kubeadm", "download cri-o", "download kubelet", "download kubectl",
		"download kubeadm", "install kubeadm", "configure kubeadm",
		"run " + kubeadmUpgradeApplyCommand("1.34.13"),
		"cordon node1",
	}
	want = append(want, nodeSwap...)
	want = append(want, "uncordon node1")
	assert.Equal(t, want, f.events)
	assert.Empty(t, f.health.problems("1.34.13", "1.34.15"))
	assert.False(t, f.health.Unschedulable)
}

func TestKubeadmUpgradeApplyCommand(t *testing.T) {
	assert.Equal(t, "sudo /opt/solo/weaver/sandbox/bin/kubeadm upgrade apply v1.34.13 --yes --v=3", kubeadmUpgradeApplyCommand("1.34.13"))
}

func TestKubernetesHop_DoneHopChangesNothing(t *testing.T) {
	f := newFakeNode(t, "1.34.13", "1.34.15", "1.34.13")

	reports := kubeHopRun(t, "1.33.13", "1.34.13", "1.34.15")

	require.Len(t, reports, 12)
	for _, r := range reports {
		want := automa.StatusSkipped
		if r.Id == "wait-kubernetes-healthy" {
			want = automa.StatusSuccess // the health gate still passes
		}
		assert.Equal(t, want, r.Status, r.Id)
	}
	assert.Empty(t, f.events)
}

func TestKubernetesHop_ResumeSkipsTheApply(t *testing.T) {
	f := newFakeNode(t, "1.34.13", "1.34.15", "1.35.10")
	f.health = healthyNode("1.35.10", "1.34.15")
	f.health.Kubelet = "1.34.13"
	f.binaries[software.KubeadmBinaryName] = "1.35.10"

	reports := kubeHopRun(t, "1.34.13", "1.35.10", "1.35.11")

	requireAllSucceeded(t, reports)
	want := []string{"download cri-o", "download kubelet", "download kubectl", "cordon node1"}
	want = append(want, nodeSwap...)
	want = append(want, "uncordon node1")
	assert.Equal(t, want, f.events)
	assert.Empty(t, f.health.problems("1.35.10", "1.35.11"))
}

func TestKubernetesHop_UnhealthyBeforeChangesNothing(t *testing.T) {
	for name, edit := range map[string]func(*kubeHealth){
		"node not Ready":  func(h *kubeHealth) { h.Ready = false },
		"kube-proxy":      func(h *kubeHealth) { h.KubeProxy = true },
		"two nodes":       func(h *kubeHealth) { h.Nodes = 2 },
		"elsewhere":       func(h *kubeHealth) { h.APIServer = "1.32.9" },
		"scheduler down":  func(h *kubeHealth) { h.ControlPlane["kube-scheduler"] = staticPod{Version: "1.33.13"} },
		"etcd missing":    func(h *kubeHealth) { delete(h.ControlPlane, "etcd") },
		"kubelet unknown": func(h *kubeHealth) { h.Kubelet = "" },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeNode(t, "1.33.13", "1.33.13", "1.34.13")
			edit(&f.health)

			reports := kubeHopRun(t, "1.33.13", "1.34.13", "1.34.15")

			require.Len(t, reports, 1)
			assert.Equal(t, automa.StatusFailed, reports[0].Status)
			assert.Contains(t, reports[0].Error.Error(), "cannot start the 1.33.13 -> 1.34.13 upgrade")
			assert.Empty(t, f.events)
		})
	}
}

func TestKubernetesHop_WaitsForASettlingControlPlane(t *testing.T) {
	f := newFakeNode(t, "1.33.13", "1.33.13", "1.34.13")
	f.settling = 3

	requireAllSucceeded(t, kubeHopRun(t, "1.33.13", "1.34.13", "1.34.15"))
	assert.Equal(t, 0, f.settling)
}

func TestKubernetesHop_FailedDownloadChangesNothing(t *testing.T) {
	f := newFakeNode(t, "1.33.13", "1.33.13", "1.34.13")
	f.failOn["download kubelet"] = assert.AnError

	reports := kubeHopRun(t, "1.33.13", "1.34.13", "1.34.15")

	require.Len(t, reports, 2)
	assert.Equal(t, automa.StatusFailed, reports[1].Status)
	assert.Contains(t, reports[1].Error.Error(), assert.AnError.Error())
	assert.Equal(t, []string{"download kubeadm", "download cri-o", "download kubelet"}, f.events)
	assert.Equal(t, "1.33.13", f.binaries[software.KubeadmBinaryName])
	assert.Equal(t, "1.33.13", f.health.APIServer)
}

func TestKubernetesHop_FailedApplyStopsBeforeTheNode(t *testing.T) {
	f := newFakeNode(t, "1.33.13", "1.33.13", "1.34.13")
	f.failOn["run "+kubeadmUpgradeApplyCommand("1.34.13")] = assert.AnError

	reports := kubeHopRun(t, "1.33.13", "1.34.13", "1.34.15")

	require.Len(t, reports, 4)
	assert.Equal(t, automa.StatusFailed, reports[3].Status)
	assert.Contains(t, reports[3].Error.Error(), "kubeadm upgrade apply 1.34.13 failed")
	assert.NotContains(t, f.events, "cordon node1")
	assert.NotContains(t, f.events, "stop kubelet")
}

func TestKubernetesHop_ApplyRefusesAKubeadmThatIsNotTheTarget(t *testing.T) {
	f := newFakeNode(t, "1.33.13", "1.33.13", "1.34.13")
	all := KubernetesHopSteps("1.33.13", "1.34.13", "1.34.15", func() (*kube.Client, error) { return &kube.Client{}, nil })
	for _, r := range runSteps(t, all[:3]) { // check, download, upgrade kubeadm
		require.Equal(t, automa.StatusSuccess, r.Status, r.Id)
	}
	f.binaries[software.KubeadmBinaryName] = "1.33.13" // replaced after its upgrade step

	reports := runSteps(t, all[3:4])

	require.Len(t, reports, 1)
	assert.Equal(t, automa.StatusFailed, reports[0].Status)
	assert.Contains(t, reports[0].Error.Error(), "the installed kubeadm is not the catalog's 1.34.13")
	assert.NotContains(t, f.events, "run "+kubeadmUpgradeApplyCommand("1.34.13"))
}

func TestKubernetesHop_RerunFinishesAStoppedHop(t *testing.T) {
	f := newFakeNode(t, "1.33.13", "1.33.13", "1.34.13")
	f.failOn["install kubelet"] = assert.AnError

	first := kubeHopRun(t, "1.33.13", "1.34.13", "1.34.15")
	require.Equal(t, automa.StatusFailed, first[len(first)-1].Status)
	// What the workflow's failure handler does: the old kubelet runs again on
	// the new cri-o, and the hop's cordon stays until the re-run lifts it.
	startService(context.Background(), software.CrioServiceName)    //nolint:errcheck
	startService(context.Background(), software.KubeletServiceName) //nolint:errcheck
	f.events = nil

	reports := kubeHopRun(t, "1.33.13", "1.34.13", "1.34.15")

	requireAllSucceeded(t, reports)
	assert.Equal(t, []string{
		"download kubelet", "download kubectl",
		"stop kubelet", "stop crio",
		"download kubelet", "install kubelet", "configure kubelet",
		"download kubectl", "install kubectl", "configure kubectl",
		"daemon-reload", "start crio", "restart kubelet",
		"uncordon node1",
	}, f.events, "no second apply, cordon or cri-o install")
	assert.Empty(t, f.health.problems("1.34.13", "1.34.15"))
}

func TestKubernetesHop_OperatorCordonIsKept(t *testing.T) {
	f := newFakeNode(t, "1.33.13", "1.33.13", "1.34.13")
	f.health.Unschedulable = true

	reports := kubeHopRun(t, "1.33.13", "1.34.13", "1.34.15")

	requireAllSucceeded(t, reports)
	assert.NotContains(t, f.events, "cordon node1")
	assert.NotContains(t, f.events, "uncordon node1")
	assert.True(t, f.health.Unschedulable)
}

func TestKubernetesHop_RestoresTheProxyRegistries(t *testing.T) {
	f := newFakeNode(t, "1.33.13", "1.33.13", "1.34.13")
	f.registry = "localhost:5050"

	requireAllSucceeded(t, kubeHopRun(t, "1.33.13", "1.34.13", "1.34.15"))

	i := slicesIndex(f.events, "registries localhost:5050")
	require.GreaterOrEqual(t, i, 0)
	assert.Less(t, slicesIndex(f.events, "install cri-o"), i)
	assert.Less(t, i, slicesIndex(f.events, "start crio"))
}

func slicesIndex(s []string, v string) int {
	for i, e := range s {
		if e == v {
			return i
		}
	}
	return -1
}

func TestKubernetesHop_UnhealthyAfterFails(t *testing.T) {
	f := newFakeNode(t, "1.33.13", "1.33.13", "1.34.13")
	f.kubeletStalls = true

	reports := kubeHopRun(t, "1.33.13", "1.34.13", "1.34.15")

	last := reports[len(reports)-1]
	assert.Equal(t, "wait-kubernetes-healthy", last.Id)
	assert.Equal(t, automa.StatusFailed, last.Status)
	assert.Contains(t, last.Error.Error(), "Kubernetes 1.34.13 is not healthy after")
	assert.True(t, f.health.OurCordon, "the cordon is lifted by the failure handler, not here")
}

func TestRecoverKubernetesHop(t *testing.T) {
	client := func() (*kube.Client, error) { return &kube.Client{}, nil }

	t.Run("lifts the hop's cordon", func(t *testing.T) {
		f := newFakeNode(t, "1.33.13", "1.33.13", "1.34.13")
		f.health.Unschedulable, f.health.OurCordon = true, true

		RecoverKubernetesHop(context.Background(), client)

		assert.Equal(t, []string{"start crio", "start kubelet", "uncordon node1"}, f.events)
		assert.False(t, f.health.Unschedulable)
	})
	t.Run("keeps an operator's cordon", func(t *testing.T) {
		f := newFakeNode(t, "1.33.13", "1.33.13", "1.34.13")
		f.health.Unschedulable = true

		RecoverKubernetesHop(context.Background(), client)

		assert.Equal(t, []string{"start crio", "start kubelet"}, f.events)
		assert.True(t, f.health.Unschedulable)
	})
	t.Run("a start failure does not stop the rest", func(t *testing.T) {
		f := newFakeNode(t, "1.33.13", "1.33.13", "1.34.13")
		f.health.OurCordon = true
		f.failOn["start crio"] = assert.AnError

		RecoverKubernetesHop(context.Background(), client)

		assert.Equal(t, []string{"start crio", "start kubelet", "uncordon node1"}, f.events)
	})
}

func TestKubeHealth_Problems(t *testing.T) {
	assert.Empty(t, healthyNode("1.34.13", "1.34.15").problems("1.34.13", "1.34.15"))

	h := healthyNode("1.34.13", "1.34.15")
	h.KubeProxy = true
	h.Runtime = "cri-o://1.33.13"
	h.ControlPlane["kube-controller-manager"] = staticPod{Version: "1.33.13", Ready: true}
	assert.ElementsMatch(t, []string{
		"a kube-proxy DaemonSet exists, but the cluster runs Cilium without kube-proxy",
		`the runtime is "cri-o://1.33.13", not cri-o://1.34.15`,
		`kube-controller-manager runs "1.33.13", not 1.34.13`,
	}, h.problems("1.34.13", "1.34.15"))

	start := healthyNode("1.34.13", "1.34.15")
	start.Kubelet = "1.33.13"
	assert.Empty(t, start.startProblems("1.33.13", "1.34.13"), "a hop stopped after the apply can run again")
}
