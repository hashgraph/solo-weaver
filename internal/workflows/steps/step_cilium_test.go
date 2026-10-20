// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
)

// Test_bandwidthManagerEnabled covers the decision the StartCilium guard makes on
// the cilium-config enable-bandwidth-manager value to assert the BN traffic
// shaper precondition (Bandwidth Manager off, issue #741). The ConfigMap value is
// the load-bearing contract, so these cases pin how it maps to enabled/disabled.
func Test_bandwidthManagerEnabled(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "true", value: "true", want: true},
		{name: "mixed case true", value: "True", want: true},
		{name: "true with whitespace", value: "  true ", want: true},
		{name: "false", value: "false", want: false},
		{name: "absent key (empty)", value: "", want: false},
		{name: "disabled literal", value: "disabled", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, bandwidthManagerEnabled(tt.value))
		})
	}
}

// ciliumCluster is a fake kube-system holding objs.
func ciliumCluster(objs ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		{Version: "v1", Resource: "configmaps"}:                 "ConfigMapList",
		{Version: "v1", Resource: "secrets"}:                    "SecretList",
		{Group: "apps", Version: "v1", Resource: "daemonsets"}:  "DaemonSetList",
		{Group: "apps", Version: "v1", Resource: "deployments"}: "DeploymentList",
	}, objs...)
}

func kubeSystemObject(apiVersion, kind, name string, labels map[string]string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion(apiVersion)
	u.SetKind(kind)
	u.SetNamespace("kube-system")
	u.SetName(name)
	u.SetLabels(labels)
	return u
}

// degradedAgent is a cilium DaemonSet with no ready pod, the state in which
// `cilium status` fails.
func degradedAgent() *unstructured.Unstructured {
	ds := kubeSystemObject("apps/v1", "DaemonSet", "cilium", nil)
	_ = unstructured.SetNestedField(ds.Object, int64(1), "status", "desiredNumberScheduled")
	_ = unstructured.SetNestedField(ds.Object, int64(0), "status", "numberReady")
	return ds
}

func clientFor(dyn *dynamicfake.FakeDynamicClient) kube.ClientProvider {
	return func() (*kube.Client, error) { return &kube.Client{Dyn: dyn}, nil }
}

const verifiedChartDir = "/tmp/solo-provisioner-cilium-chart-1234/cilium"

// ciliumCNIHarness records every cilium CLI command, binary verification and
// chart download.
type ciliumCNIHarness struct {
	scripts      []string
	verified     *[]string
	charts       []ciliumRelease
	chartErr     error
	chartRemoved int
}

func newCiliumCNIHarness(t *testing.T, verify func(string) error) *ciliumCNIHarness {
	t.Helper()
	h := &ciliumCNIHarness{}
	h.verified = stubVerifyExecutables(t, verify)
	origRun, origFetch := runCiliumScript, fetchCiliumChart
	runCiliumScript = func(scripts []string, _ string) (string, error) {
		h.scripts = append(h.scripts, strings.Join(scripts, " "))
		return "", nil
	}
	fetchCiliumChart = func(_ context.Context, release ciliumRelease) (string, func(), error) {
		h.charts = append(h.charts, release)
		if h.chartErr != nil {
			return "", nil, h.chartErr
		}
		return verifiedChartDir, func() { h.chartRemoved++ }, nil
	}
	t.Cleanup(func() { runCiliumScript, fetchCiliumChart = origRun, origFetch })
	return h
}

func TestInstallCiliumCNI_FreshClusterInstallsPinnedVersion(t *testing.T) {
	h := newCiliumCNIHarness(t, func(string) error { return nil })

	step, err := installCiliumCNI(newClusterCilium, clientFor(ciliumCluster())).Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())

	require.NoError(t, report.Error)
	assert.Equal(t, automa.StatusSuccess, report.Status)
	assert.Equal(t, []string{"cilium"}, *h.verified)
	assert.Equal(t, []ciliumRelease{newClusterCilium}, h.charts)
	require.Len(t, h.scripts, 1)
	assert.Contains(t, h.scripts[0], `cilium install --wait --version "1.20.2" --chart-directory '`+verifiedChartDir+`' --values `)
	installed, _ := step.State().Local().Bool(InstalledByThisStep)
	assert.True(t, installed)
	assert.Equal(t, 1, h.chartRemoved, "the unpacked chart is removed once cilium install returns")
}

func TestInstallCiliumCNI_ChartIsRemovedWhenInstallFails(t *testing.T) {
	h := newCiliumCNIHarness(t, func(string) error { return nil })
	runCiliumScript = func([]string, string) (string, error) { return "", assert.AnError }

	step, err := installCiliumCNI(newClusterCilium, clientFor(ciliumCluster())).Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())

	require.ErrorIs(t, report.Error, assert.AnError)
	assert.Equal(t, 1, h.chartRemoved)
}

func TestInstallCiliumCNI_UnverifiedChartIsNotInstalled(t *testing.T) {
	h := newCiliumCNIHarness(t, func(string) error { return nil })
	h.chartErr = assert.AnError

	step, err := installCiliumCNI(newClusterCilium, clientFor(ciliumCluster())).Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())

	require.ErrorIs(t, report.Error, assert.AnError)
	assert.Equal(t, automa.StatusFailed, report.Status)
	assert.Empty(t, h.scripts, "cilium install must not run without a verified chart")
	installed, _ := step.State().Local().Bool(InstalledByThisStep)
	assert.False(t, installed)

	rollback := step.Rollback(context.Background())
	assert.Equal(t, automa.StatusSkipped, rollback.Status)
	assert.Empty(t, h.scripts)
}

func TestInstallCiliumCNI_ExistingCiliumIsLeftAlone(t *testing.T) {
	cases := map[string][]runtime.Object{
		"healthy install": {
			kubeSystemObject("v1", "ConfigMap", "cilium-config", nil),
			kubeSystemObject("apps/v1", "DaemonSet", "cilium", nil),
			kubeSystemObject("apps/v1", "Deployment", "cilium-operator", nil),
		},
		"degraded agent": {
			kubeSystemObject("v1", "ConfigMap", "cilium-config", nil),
			degradedAgent(),
		},
		"config map missing, agent still deployed": {degradedAgent()},
		"only the operator":                        {kubeSystemObject("apps/v1", "Deployment", "cilium-operator", nil)},
		"only a failed helm release": {
			kubeSystemObject("v1", "Secret", "sh.helm.release.v1.cilium.v1", map[string]string{"owner": "helm", "name": "cilium", "status": "failed"}),
		},
	}
	for name, objs := range cases {
		t.Run(name, func(t *testing.T) {
			// A broken or missing CLI must not matter: nothing is executed.
			h := newCiliumCNIHarness(t, func(string) error { return assert.AnError })

			step, err := installCiliumCNI(newClusterCilium, clientFor(ciliumCluster(objs...))).Build()
			require.NoError(t, err)
			report := step.Execute(context.Background())

			require.NoError(t, report.Error)
			assert.Equal(t, automa.StatusSkipped, report.Status)
			assert.Equal(t, "true", report.Metadata[AlreadyInstalled])
			assert.NotEmpty(t, report.Metadata[CiliumResources])
			assert.Empty(t, *h.verified, "the cilium binary is not needed to skip")
			assert.Empty(t, h.charts, "no chart is needed to skip")
			assert.Empty(t, h.scripts, "no cilium command may run against an existing install")

			rollback := step.Rollback(context.Background())
			assert.Equal(t, automa.StatusSkipped, rollback.Status, "rollback must not uninstall a Cilium this step did not install")
			assert.Empty(t, h.scripts)
		})
	}
}

func TestInstallCiliumCNI_ExistingCiliumWinsOverFailedLookups(t *testing.T) {
	h := newCiliumCNIHarness(t, func(string) error { return assert.AnError })
	dyn := ciliumCluster(kubeSystemObject("v1", "ConfigMap", "cilium-config", nil))
	dyn.PrependReactor("list", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, kerrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, "", fmt.Errorf("rbac"))
	})

	step, err := installCiliumCNI(newClusterCilium, clientFor(dyn)).Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())

	require.NoError(t, report.Error)
	assert.Equal(t, automa.StatusSkipped, report.Status)
	assert.Equal(t, "configmaps/cilium-config", report.Metadata[CiliumResources])
	assert.Empty(t, h.scripts)
}

func TestInstallCiliumCNI_DetectionFailureDoesNotInstall(t *testing.T) {
	cases := map[string]struct {
		provider func() kube.ClientProvider
		reason   errx.Reason
	}{
		"no kubeconfig": {
			provider: func() kube.ClientProvider {
				return func() (*kube.Client, error) { return nil, fmt.Errorf("no kubeconfig") }
			},
			reason: reasons.K8sAPIFailed,
		},
		"api unreachable": {
			provider: func() kube.ClientProvider {
				return clientFor(failing("get", "configmaps", fmt.Errorf("dial tcp 10.0.0.1:6443: connect: connection refused")))
			},
			reason: reasons.K8sAPIFailed,
		},
		"forbidden": {
			provider: func() kube.ClientProvider {
				return clientFor(failing("get", "daemonsets", kerrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "daemonsets"}, "cilium", fmt.Errorf("rbac"))))
			},
			reason: reasons.K8sAPIFailed,
		},
		"timeout": {
			provider: func() kube.ClientProvider {
				return clientFor(failing("list", "secrets", kerrors.NewTimeoutError("deadline exceeded", 1)))
			},
			reason: reasons.K8sAPIFailed,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newCiliumCNIHarness(t, func(string) error { return nil })

			step, err := installCiliumCNI(newClusterCilium, tc.provider()).Build()
			require.NoError(t, err)
			report := step.Execute(context.Background())

			require.Error(t, report.Error)
			assert.Equal(t, automa.StatusFailed, report.Status)
			assert.Contains(t, report.Error.Error(), "'cilium install' was not run")
			reason, _ := errx.ReasonOf(report.Error)
			assert.Equal(t, tc.reason, reason)
			hints, _ := errx.Hints(report.Error)
			assert.NotEmpty(t, hints)
			assert.Empty(t, h.scripts, "a failed lookup must never lead to an install")
			installed, _ := step.State().Local().Bool(InstalledByThisStep)
			assert.False(t, installed)
		})
	}
}

func failing(verb, resource string, err error) *dynamicfake.FakeDynamicClient {
	dyn := ciliumCluster()
	dyn.PrependReactor(verb, resource, func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, err
	})
	return dyn
}

// The step verifies the cilium binary before shelling out to it on both install
// and rollback, so a failed checksum aborts before the binary is executed.

func TestInstallCiliumCNI_FailsWhenCiliumVerificationFails(t *testing.T) {
	h := newCiliumCNIHarness(t, func(string) error { return assert.AnError })

	step, err := installCiliumCNI(newClusterCilium, clientFor(ciliumCluster())).Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.Error(t, report.Error)
	assert.Equal(t, automa.StatusFailed, report.Status)
	require.ErrorIs(t, report.Error, assert.AnError)
	assert.Equal(t, []string{"cilium"}, *h.verified)
	assert.Empty(t, h.charts)
	assert.Empty(t, h.scripts)
}

func TestInstallCiliumCNI_RollbackFailsWhenCiliumVerificationFails(t *testing.T) {
	h := newCiliumCNIHarness(t, func(string) error { return assert.AnError })

	step, err := installCiliumCNI(newClusterCilium, clientFor(ciliumCluster())).Build()
	require.NoError(t, err)

	// Rollback is a no-op unless this step performed the install; mark it so
	// rollback proceeds past that guard and reaches the checksum verification.
	step.State().Local().Set(InstalledByThisStep, true)

	report := step.Rollback(context.Background())
	require.Error(t, report.Error)
	assert.Equal(t, automa.StatusFailed, report.Status)
	require.ErrorIs(t, report.Error, assert.AnError)
	assert.Empty(t, h.scripts)
}
