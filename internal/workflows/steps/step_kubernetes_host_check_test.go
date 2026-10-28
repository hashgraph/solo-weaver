// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/golang/mock/gomock"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

const (
	k8sOld  = "1.33.4"
	k8sNew  = "1.36.5"
	crioNew = "1.36.7"
)

// stubCgroupV2 reports the given cgroup mode and counts how often it was asked.
func stubCgroupV2(t *testing.T, enabled bool, err error) *int {
	t.Helper()
	calls := 0
	orig := cgroupV2Enabled
	cgroupV2Enabled = func() (bool, error) {
		calls++
		return enabled, err
	}
	t.Cleanup(func() { cgroupV2Enabled = orig })
	return &calls
}

func installedAt(name, target, version string) kubernetesHostComponent {
	return kubernetesHostComponent{name: name, target: target, installed: true, version: version}
}

func missing(name, target string) kubernetesHostComponent {
	return kubernetesHostComponent{name: name, target: target, pending: true}
}

func existing133Host() []kubernetesHostComponent {
	return []kubernetesHostComponent{
		installedAt("kubelet", k8sNew, k8sOld),
		installedAt("kubeadm", k8sNew, k8sOld),
		installedAt("kubectl", k8sNew, k8sOld),
		installedAt("cri-o", crioNew, k8sOld),
	}
}

func freshHost() []kubernetesHostComponent {
	return []kubernetesHostComponent{
		missing("kubelet", k8sNew),
		missing("kubeadm", k8sNew),
		missing("kubectl", k8sNew),
		missing("cri-o", crioNew),
	}
}

func requirePreconditionNotMet(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	reason, ok := errx.ReasonOf(err)
	require.True(t, ok, "missing reason on %v", err)
	assert.Equal(t, reasons.PreconditionNotMet, reason)
	hints, ok := errx.Hints(err)
	require.True(t, ok)
	assert.NotEmpty(t, hints)
}

func TestCheckKubernetesHost_FreshHostWithCgroupV2Passes(t *testing.T) {
	calls := stubCgroupV2(t, true, nil)
	require.NoError(t, checkKubernetesHostComponents(freshHost(), existingControlPlane{}))
	assert.Equal(t, 1, *calls)
}

func TestCheckKubernetesHost_FreshHostWithCgroupV1Fails(t *testing.T) {
	stubCgroupV2(t, false, nil)
	err := checkKubernetesHostComponents(freshHost(), existingControlPlane{})
	requirePreconditionNotMet(t, err)
	assert.Contains(t, err.Error(), "needs cgroup v2")
}

func TestCheckKubernetesHost_CgroupProbeErrorFails(t *testing.T) {
	stubCgroupV2(t, false, errorx.ExternalError.New("stat failed"))
	err := checkKubernetesHostComponents(freshHost(), existingControlPlane{})
	requirePreconditionNotMet(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.ExternalError), "the probe failure must stay visible: %v", err)
}

func TestCheckKubernetesHost_Existing133HostPassesUntouched(t *testing.T) {
	// Nothing would be installed, so neither the version nor the cgroup rule
	// applies: an existing cgroup v1 node keeps working with this provisioner.
	calls := stubCgroupV2(t, false, nil)
	require.NoError(t, checkKubernetesHostComponents(existing133Host(), existingControlPlane{}))
	assert.Zero(t, *calls)
}

func TestCheckKubernetesHost_UnknownVersionWithNothingToInstallPasses(t *testing.T) {
	// VerifyExecutables refuses to run such binaries later; this check only
	// guards what the run would install.
	stubCgroupV2(t, true, nil)
	host := existing133Host()
	host[0].version = ""
	require.NoError(t, checkKubernetesHostComponents(host, existingControlPlane{}))
}

func TestCheckKubernetesHost_MissingComponentNextToOlderVersionFails(t *testing.T) {
	stubCgroupV2(t, true, nil)
	cases := map[string]int{"kubelet": 0, "kubeadm": 1, "kubectl": 2, "cri-o": 3}
	for name, i := range cases {
		t.Run(name, func(t *testing.T) {
			host := existing133Host()
			host[i] = missing(host[i].name, host[i].target)

			err := checkKubernetesHostComponents(host, existingControlPlane{})
			requirePreconditionNotMet(t, err)
			assert.Contains(t, err.Error(), "partly installed")
			assert.Contains(t, err.Error(), host[i].name+" "+host[i].target)
		})
	}
}

func TestCheckKubernetesHost_UnconfiguredOlderKubeadmFails(t *testing.T) {
	// Configuring kubeadm renders kubeadm-init.yaml with the catalog default,
	// which would put 1.36.5 into an existing 1.33.4 cluster's init config.
	stubCgroupV2(t, true, nil)
	host := existing133Host()
	host[1].pending = true

	err := checkKubernetesHostComponents(host, existingControlPlane{})
	requirePreconditionNotMet(t, err)
	assert.Contains(t, err.Error(), "kubeadm 1.36.5")
}

func TestCheckKubernetesHost_UnknownVersionWithMissingComponentFails(t *testing.T) {
	stubCgroupV2(t, true, nil)
	host := freshHost()
	host[0] = installedAt("kubelet", k8sNew, "")

	err := checkKubernetesHostComponents(host, existingControlPlane{})
	requirePreconditionNotMet(t, err)
	assert.Contains(t, err.Error(), "kubelet unknown version")
}

func TestCheckKubernetesHost_ResumingAFreshInstallPasses(t *testing.T) {
	// An earlier run of this provisioner stopped half way: what it installed is
	// already at the catalog default, so finishing the install is safe.
	stubCgroupV2(t, true, nil)
	host := freshHost()
	host[0] = installedAt("kubelet", k8sNew, k8sNew)
	host[3] = installedAt("cri-o", crioNew, crioNew)
	require.NoError(t, checkKubernetesHostComponents(host, existingControlPlane{}))
}

func TestCheckKubernetesHost_InstalledNewKubeletOnCgroupV1Fails(t *testing.T) {
	stubCgroupV2(t, false, nil)
	host := freshHost()
	host[0] = installedAt("kubelet", k8sNew, k8sNew)
	requirePreconditionNotMet(t, checkKubernetesHostComponents(host, existingControlPlane{}))
}

func TestKubeletNeedsCgroupV2(t *testing.T) {
	for version, want := range map[string]bool{"1.33.4": false, "1.34.9": false, "1.35.0": true, "1.36.5": true, "": false} {
		got, err := kubeletNeedsCgroupV2(version)
		require.NoError(t, err)
		assert.Equal(t, want, got, version)
	}
	_, err := kubeletNeedsCgroupV2("not-a-version")
	require.Error(t, err)
}

type fakeMachineRuntime map[string]state.SoftwareState

func (f fakeMachineRuntime) SoftwareState(name string) (state.SoftwareState, bool) {
	sw, ok := f[name]
	return sw, ok
}

// fakeInstaller answers the questions the setup steps ask an installer.
func fakeInstaller(t *testing.T, name, target string, installed, configured bool) func(...software.InstallerOption) (software.Software, error) {
	t.Helper()
	inst := software.NewMockSoftware(gomock.NewController(t))
	inst.EXPECT().GetSoftwareName().Return(name).AnyTimes()
	inst.EXPECT().Version().Return(target).AnyTimes()
	inst.EXPECT().IsInstalled().Return(installed, nil).AnyTimes()
	inst.EXPECT().IsConfigured().Return(configured, nil).AnyTimes()
	return func(...software.InstallerOption) (software.Software, error) { return inst, nil }
}

func TestCollectKubernetesHostComponents_UsesRuntimeVersions(t *testing.T) {
	mr := fakeMachineRuntime{
		"kubelet": {Name: "kubelet", Version: k8sOld, Installed: true},
		"kubeadm": {Name: "kubeadm", Version: k8sOld, Installed: true},
	}
	sources := []kubernetesHostSource{
		{"kubelet", fakeInstaller(t, "kubelet", k8sNew, true, true)},
		{"kubeadm", fakeInstaller(t, "kubeadm", k8sNew, true, false)},
		{"crio", fakeInstaller(t, "cri-o", crioNew, false, false)},
	}

	got, err := collectKubernetesHostComponents(mr, sources)
	require.NoError(t, err)
	assert.Equal(t, []kubernetesHostComponent{
		{name: "kubelet", target: k8sNew, installed: true, version: k8sOld},
		{name: "kubeadm", target: k8sNew, installed: true, version: k8sOld, pending: true},
		{name: "cri-o", target: crioNew, pending: true},
	}, got)
}

func TestCollectKubernetesHostComponents_WithoutSnapshotIdentifiesByChecksum(t *testing.T) {
	orig := identifyInstalledVersions
	identifyInstalledVersions = func(artifact string) ([]string, error) {
		return map[string][]string{"kubelet": {k8sOld}, "kubectl": {k8sOld, k8sNew}}[artifact], nil
	}
	t.Cleanup(func() { identifyInstalledVersions = orig })

	sources := []kubernetesHostSource{
		{"kubelet", fakeInstaller(t, "kubelet", k8sNew, true, true)},
		{"kubectl", fakeInstaller(t, "kubectl", k8sNew, true, true)},
	}

	got, err := collectKubernetesHostComponents(nil, sources)
	require.NoError(t, err)
	assert.Equal(t, k8sOld, got[0].version)
	assert.Empty(t, got[1].version, "an ambiguous match is not a version")
}

func TestCheckKubernetesHostStep_FailsBeforeInstalling(t *testing.T) {
	stubCgroupV2(t, true, nil)
	orig := kubernetesHostSources
	kubernetesHostSources = []kubernetesHostSource{
		{"kubelet", fakeInstaller(t, "kubelet", k8sNew, true, true)},
		{"kubeadm", fakeInstaller(t, "kubeadm", k8sNew, false, false)},
	}
	t.Cleanup(func() { kubernetesHostSources = orig })

	noControlPlane(t)
	mr := fakeMachineRuntime{"kubelet": {Name: "kubelet", Version: k8sOld, Installed: true}}
	step, err := CheckKubernetesHost(mr).Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.Error(t, report.Error)
	assert.Equal(t, automa.StatusFailed, report.Status)
}

// apiServerManifest is the shape kubeadm writes for the kube-apiserver static pod.
func apiServerManifest(version string) string {
	return `apiVersion: v1
kind: Pod
metadata:
  annotations:
    kubeadm.kubernetes.io/kube-apiserver.advertise-address.endpoint: 10.0.0.5:6443
  labels:
    component: kube-apiserver
    tier: control-plane
  name: kube-apiserver
  namespace: kube-system
spec:
  containers:
  - command:
    - kube-apiserver
    - --advertise-address=10.0.0.5
    image: registry.k8s.io/kube-apiserver:v` + version + `
    name: kube-apiserver
  hostNetwork: true
`
}

// leftoverControlPlane writes the files kubeadm init leaves under a
// Kubernetes config dir and points the check at it.
func leftoverControlPlane(t *testing.T, version string, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range files {
		p := filepath.Join(dir, f)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		content := "placeholder"
		if f == "manifests/kube-apiserver.yaml" {
			content = apiServerManifest(version)
		}
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	}
	orig := kubernetesConfigDirs
	kubernetesConfigDirs = func() []string { return []string{filepath.Join(t.TempDir(), "empty"), dir} }
	t.Cleanup(func() { kubernetesConfigDirs = orig })
	return dir
}

func noControlPlane(t *testing.T) {
	t.Helper()
	orig := kubernetesConfigDirs
	kubernetesConfigDirs = func() []string { return []string{t.TempDir(), t.TempDir()} }
	t.Cleanup(func() { kubernetesConfigDirs = orig })
}

// stubAPIServer makes the API probe answer err and records the kubeconfigs it
// was given.
func stubAPIServer(t *testing.T, err error) *[]string {
	t.Helper()
	calls := &[]string{}
	orig := probeAPIServer
	probeAPIServer = func(kubeconfig string) error {
		*calls = append(*calls, kubeconfig)
		return err
	}
	t.Cleanup(func() { probeAPIServer = orig })
	return calls
}

var allControlPlaneFiles = []string{
	"admin.conf",
	"manifests/kube-apiserver.yaml",
	"manifests/etcd.yaml",
	"manifests/kube-controller-manager.yaml",
	"manifests/kube-scheduler.yaml",
}

func detect(t *testing.T) existingControlPlane {
	t.Helper()
	cp, err := detectControlPlane(kubernetesConfigDirs())
	require.NoError(t, err)
	return cp
}

func TestDetectControlPlane(t *testing.T) {
	t.Run("fresh host", func(t *testing.T) {
		noControlPlane(t)
		assert.Empty(t, detect(t).evidence)
	})
	t.Run("kubeadm leftovers", func(t *testing.T) {
		dir := leftoverControlPlane(t, k8sOld, allControlPlaneFiles...)
		cp := detect(t)
		assert.Len(t, cp.evidence, 5)
		assert.Equal(t, k8sOld, cp.version)
		assert.Equal(t, filepath.Join(dir, "admin.conf"), cp.kubeconfig)
	})
	t.Run("manifests only", func(t *testing.T) {
		leftoverControlPlane(t, k8sOld, "manifests/etcd.yaml", "manifests/kube-apiserver.yaml")
		cp := detect(t)
		assert.Equal(t, k8sOld, cp.version)
		assert.Empty(t, cp.kubeconfig)
	})
	t.Run("bind mount and source are counted once", func(t *testing.T) {
		dir := leftoverControlPlane(t, k8sOld, allControlPlaneFiles...)
		orig := kubernetesConfigDirs
		kubernetesConfigDirs = func() []string { return []string{dir, dir + "/."} }
		t.Cleanup(func() { kubernetesConfigDirs = orig })
		assert.Len(t, detect(t).evidence, 5)
	})
	t.Run("admin.conf only", func(t *testing.T) {
		leftoverControlPlane(t, "", "admin.conf")
		cp := detect(t)
		assert.Len(t, cp.evidence, 1)
		assert.Empty(t, cp.version)
	})
}

func TestImageTagVersion(t *testing.T) {
	for image, want := range map[string]string{
		"registry.k8s.io/kube-apiserver:v1.33.4":                 "1.33.4",
		"registry.k8s.io/kube-apiserver:v1.36.5@sha256:abcdef":   "1.36.5",
		"localhost:5000/kube-apiserver:v1.36.5":                  "1.36.5",
		"localhost:5000/kube-apiserver":                          "",
		"registry.k8s.io/kube-apiserver@sha256:abcdef0123456789": "",
	} {
		assert.Equal(t, want, imageTagVersion(image), image)
	}
}

func TestCheckKubernetesHost_FreshHostDoesNotProbeAPI(t *testing.T) {
	stubCgroupV2(t, true, nil)
	calls := stubAPIServer(t, assert.AnError)
	require.NoError(t, checkKubernetesHostComponents(freshHost(), existingControlPlane{}))
	assert.Empty(t, *calls)
}

func TestCheckKubernetesHost_HealthyExistingClusterPasses(t *testing.T) {
	stubCgroupV2(t, false, nil)
	leftoverControlPlane(t, k8sOld, allControlPlaneFiles...)
	calls := stubAPIServer(t, nil)
	require.NoError(t, checkKubernetesHostComponents(existing133Host(), detect(t)))
	assert.Len(t, *calls, 1)
}

func TestCheckKubernetesHost_ExistingClusterWithAPIDownFails(t *testing.T) {
	stubCgroupV2(t, true, nil)
	dir := leftoverControlPlane(t, k8sOld, allControlPlaneFiles...)
	calls := stubAPIServer(t, errorx.ExternalError.New("connection refused"))

	err := checkKubernetesHostComponents(existing133Host(), detect(t))
	requirePreconditionNotMet(t, err)
	assert.Contains(t, err.Error(), "API server is not reachable")
	assert.Equal(t, []string{filepath.Join(dir, "admin.conf")}, *calls)
}

func TestCheckKubernetesHost_AllBinariesMissingUnderExistingControlPlaneFails(t *testing.T) {
	// The binaries look fresh, but the control plane kubeadm left behind runs
	// 1.33.4. Installing 1.36.5 would silently upgrade that cluster's node.
	cgroupCalls := stubCgroupV2(t, true, nil)
	leftoverControlPlane(t, k8sOld, allControlPlaneFiles...)
	stubAPIServer(t, nil)

	err := checkKubernetesHostComponents(freshHost(), detect(t))
	requirePreconditionNotMet(t, err)
	assert.Contains(t, err.Error(), "version 1.33.4")
	assert.Contains(t, err.Error(), "kubelet 1.36.5")
	assert.Zero(t, *cgroupCalls)
}

func TestCheckKubernetesHost_UnknownControlPlaneVersionWithMissingComponentsFails(t *testing.T) {
	stubCgroupV2(t, true, nil)
	leftoverControlPlane(t, "", "admin.conf")
	stubAPIServer(t, nil)

	err := checkKubernetesHostComponents(freshHost(), detect(t))
	requirePreconditionNotMet(t, err)
	assert.Contains(t, err.Error(), "version unknown")
}

func TestCheckKubernetesHost_ManifestsWithoutAdminConfFail(t *testing.T) {
	stubCgroupV2(t, true, nil)
	leftoverControlPlane(t, k8sOld, "manifests/kube-apiserver.yaml", "manifests/etcd.yaml")
	calls := stubAPIServer(t, nil)

	err := checkKubernetesHostComponents(existing133Host(), detect(t))
	requirePreconditionNotMet(t, err)
	assert.Contains(t, err.Error(), "no admin.conf")
	assert.Empty(t, *calls)
}

func TestCheckKubernetesHost_RepairingAClusterAtTheDefaultVersion(t *testing.T) {
	stubCgroupV2(t, true, nil)
	leftoverControlPlane(t, k8sNew, allControlPlaneFiles...)
	host := []kubernetesHostComponent{
		installedAt("kubelet", k8sNew, k8sNew),
		installedAt("kubeadm", k8sNew, k8sNew),
		missing("kubectl", k8sNew),
		installedAt("cri-o", crioNew, crioNew),
	}

	t.Run("API up", func(t *testing.T) {
		stubAPIServer(t, nil)
		require.NoError(t, checkKubernetesHostComponents(host, detect(t)))
	})
	t.Run("API down", func(t *testing.T) {
		stubAPIServer(t, assert.AnError)
		requirePreconditionNotMet(t, checkKubernetesHostComponents(host, detect(t)))
	})
}

// The guard has to stop the workflow before any installer downloads or
// installs: the install steps below use mocks that fail on any such call.
func TestKubernetesSetup_StopsBeforeInstallingIntoLeftoverControlPlane(t *testing.T) {
	stubCgroupV2(t, true, nil)
	leftoverControlPlane(t, k8sOld, allControlPlaneFiles...)
	stubAPIServer(t, nil)

	orig := kubernetesHostSources
	t.Cleanup(func() { kubernetesHostSources = orig })
	kubernetesHostSources = nil
	var installs []automa.Builder
	for name, tc := range kubernetesInstallSteps {
		key := name
		if name == "cri-o" {
			key = software.CrioBinaryName
		}
		target := k8sNew
		if name == "cri-o" {
			target = crioNew
		}
		inst := software.NewMockSoftware(gomock.NewController(t))
		inst.EXPECT().GetSoftwareName().Return(name).AnyTimes()
		inst.EXPECT().Version().Return(target).AnyTimes()
		inst.EXPECT().IsInstalled().Return(false, nil).AnyTimes()
		inst.EXPECT().IsConfigured().Return(false, nil).AnyTimes()
		provider := func(...software.InstallerOption) (software.Software, error) { return inst, nil }
		kubernetesHostSources = append(kubernetesHostSources, kubernetesHostSource{key, provider})
		installs = append(installs, tc.step(provider, nil))
	}

	wf, err := automa.NewWorkflowBuilder().WithId("kubernetes-setup-test").
		Steps(append([]automa.Builder{CheckKubernetesHost(nil)}, installs...)...).
		WithExecutionMode(automa.StopOnError).
		Build()
	require.NoError(t, err)

	report := wf.Execute(context.Background())
	require.Error(t, report.Error)
	assert.Equal(t, automa.StatusFailed, report.Status)
	require.Len(t, report.StepReports, 1, "no install step may run after the guard fails")
	assert.Equal(t, "check-kubernetes-host", report.StepReports[0].Id)
	requirePreconditionNotMet(t, report.StepReports[0].Error)
}
