// SPDX-License-Identifier: Apache-2.0

package cilium

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/automa-saga/errx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/hashgraph/solo-weaver/pkg/reasons"
)

func fakeCluster(objs ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		secretsGVR:    "SecretList",
		daemonSetsGVR: "DaemonSetList",
	}, objs...)
}

func object(apiVersion, kind, name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion(apiVersion)
	u.SetKind(kind)
	u.SetNamespace(Namespace)
	u.SetName(name)
	return u
}

// agentDaemonSet mirrors the chart's DaemonSet: init containers and the agent
// all use the cilium image, so only the container name identifies the agent.
func agentDaemonSet(agentImage string, initImage string) *unstructured.Unstructured {
	ds := object("apps/v1", "DaemonSet", daemonSetName)
	_ = unstructured.SetNestedSlice(ds.Object, []any{
		map[string]any{"name": agentContainerName, "image": agentImage},
	}, "spec", "template", "spec", "containers")
	_ = unstructured.SetNestedSlice(ds.Object, []any{
		map[string]any{"name": "config", "image": initImage},
	}, "spec", "template", "spec", "initContainers")
	return ds
}

// releaseSecret encodes a release the way Helm's secret driver stores it.
func releaseSecret(revision int, status, chartVersion string) *unstructured.Unstructured {
	payload, _ := json.Marshal(map[string]any{
		"name":    releaseName,
		"version": revision,
		"chart":   map[string]any{"metadata": map[string]any{"name": "cilium", "version": chartVersion}},
	})
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write(payload)
	_ = zw.Close()
	helmEncoded := base64.StdEncoding.EncodeToString(gz.Bytes())

	s := object("v1", "Secret", fmt.Sprintf("sh.helm.release.v1.%s.v%d", releaseName, revision))
	s.SetLabels(map[string]string{"owner": "helm", "name": releaseName, "status": status, "version": fmt.Sprint(revision)})
	_ = unstructured.SetNestedField(s.Object, base64.StdEncoding.EncodeToString([]byte(helmEncoded)), "data", "release")
	return s
}

const image1181 = "quay.io/cilium/cilium:v1.18.1@sha256:65ab17c052d8758b2ad157ce766285e04173722df59bdee1ea6d5fda7149f0e9"

func TestInstalledVersion(t *testing.T) {
	t.Run("agent image and deployed release agree", func(t *testing.T) {
		dyn := fakeCluster(
			agentDaemonSet(image1181, image1181),
			releaseSecret(1, "superseded", "1.18.0"),
			releaseSecret(2, "deployed", "1.18.1"),
		)
		got, err := InstalledVersion(context.Background(), dyn)
		require.NoError(t, err)
		assert.Equal(t, "1.18.1", got)
	})

	t.Run("only the agent container counts", func(t *testing.T) {
		dyn := fakeCluster(
			agentDaemonSet(image1181, "quay.io/cilium/cilium:v1.20.2"),
			releaseSecret(1, "deployed", "1.18.1"),
		)
		got, err := InstalledVersion(context.Background(), dyn)
		require.NoError(t, err)
		assert.Equal(t, "1.18.1", got)
	})
}

func TestInstalledVersion_Unresolvable(t *testing.T) {
	cases := map[string]struct {
		objs   []runtime.Object
		reason errx.Reason
	}{
		"no daemonset":            {objs: []runtime.Object{releaseSecret(1, "deployed", "1.18.1")}, reason: reasons.PreconditionNotMet},
		"no agent container":      {objs: []runtime.Object{noAgentDaemonSet(), releaseSecret(1, "deployed", "1.18.1")}, reason: reasons.PreconditionNotMet},
		"moving tag":              {objs: []runtime.Object{agentDaemonSet("quay.io/cilium/cilium:latest", image1181), releaseSecret(1, "deployed", "1.18.1")}, reason: reasons.PreconditionNotMet},
		"digest only":             {objs: []runtime.Object{agentDaemonSet("quay.io/cilium/cilium@sha256:65ab17c052d8758b2ad157ce766285e04173722df59bdee1ea6d5fda7149f0e9", image1181), releaseSecret(1, "deployed", "1.18.1")}, reason: reasons.PreconditionNotMet},
		"registry port, no tag":   {objs: []runtime.Object{agentDaemonSet("registry.local:5000/cilium/cilium", image1181), releaseSecret(1, "deployed", "1.18.1")}, reason: reasons.PreconditionNotMet},
		"no deployed release":     {objs: []runtime.Object{agentDaemonSet(image1181, image1181), releaseSecret(1, "failed", "1.18.1")}, reason: reasons.PreconditionNotMet},
		"two deployed releases":   {objs: []runtime.Object{agentDaemonSet(image1181, image1181), releaseSecret(1, "deployed", "1.18.1"), releaseSecret(2, "deployed", "1.18.1")}, reason: reasons.PreconditionNotMet},
		"agent and chart differ":  {objs: []runtime.Object{agentDaemonSet(image1181, image1181), releaseSecret(1, "deployed", "1.20.2")}, reason: reasons.PreconditionNotMet},
		"invalid chart version":   {objs: []runtime.Object{agentDaemonSet(image1181, image1181), releaseSecret(1, "deployed", "1.18")}, reason: reasons.PreconditionNotMet},
		"corrupt release payload": {objs: []runtime.Object{agentDaemonSet(image1181, image1181), corruptReleaseSecret()}, reason: reasons.PreconditionNotMet},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := InstalledVersion(context.Background(), fakeCluster(tc.objs...))
			require.Error(t, err)
			assert.Empty(t, got)
			assertDecorated(t, err, tc.reason)
		})
	}

	t.Run("api error", func(t *testing.T) {
		dyn := fakeCluster(agentDaemonSet(image1181, image1181), releaseSecret(1, "deployed", "1.18.1"))
		dyn.PrependReactor("get", "daemonsets", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, kerrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "daemonsets"}, daemonSetName, fmt.Errorf("rbac"))
		})
		got, err := InstalledVersion(context.Background(), dyn)
		require.Error(t, err)
		assert.Empty(t, got)
		assertDecorated(t, err, reasons.K8sAPIFailed)
	})
}

func assertDecorated(t *testing.T, err error, want errx.Reason) {
	t.Helper()
	reason, ok := errx.ReasonOf(err)
	assert.True(t, ok, "error carries no reason: %v", err)
	assert.Equal(t, want, reason)
	hints, _ := errx.Hints(err)
	assert.NotEmpty(t, hints)
}

func noAgentDaemonSet() *unstructured.Unstructured {
	ds := object("apps/v1", "DaemonSet", daemonSetName)
	_ = unstructured.SetNestedSlice(ds.Object, []any{
		map[string]any{"name": "sidecar", "image": image1181},
	}, "spec", "template", "spec", "containers")
	return ds
}

func corruptReleaseSecret() *unstructured.Unstructured {
	s := releaseSecret(1, "deployed", "1.18.1")
	_ = unstructured.SetNestedField(s.Object, base64.StdEncoding.EncodeToString([]byte("not-helm")), "data", "release")
	return s
}

func TestImageVersion(t *testing.T) {
	cases := map[string]string{
		"quay.io/cilium/cilium:v1.18.1":           "1.18.1",
		"quay.io/cilium/cilium:1.20.2":            "1.20.2",
		image1181:                                 "1.18.1",
		"registry.local:5000/cilium:v1.20.0-rc.1": "1.20.0-rc.1",
	}
	for image, want := range cases {
		got, ok := imageVersion(image)
		assert.True(t, ok, image)
		assert.Equal(t, want, got, image)
	}

	for _, image := range []string{"quay.io/cilium/cilium", "quay.io/cilium/cilium:stable", "registry.local:5000/cilium", "cilium:v1.18"} {
		_, ok := imageVersion(image)
		assert.False(t, ok, image)
	}
}
