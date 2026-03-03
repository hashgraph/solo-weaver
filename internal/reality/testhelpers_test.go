// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"context"
	"os"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	helmtime "helm.sh/helm/v3/pkg/time"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// fakeStateManager is a minimal state.Manager that returns a controlled State.
type fakeStateManager struct {
	state state.State
}

func (f *fakeStateManager) State() state.State                                      { return f.state }
func (f *fakeStateManager) HasPersistedState() (os.FileInfo, bool, error)           { return nil, false, nil }
func (f *fakeStateManager) Set(s state.State) state.Writer                          { f.state = s; return f }
func (f *fakeStateManager) AddActionHistory(entry state.ActionHistory) state.Writer { return f }
func (f *fakeStateManager) Flush() error                                            { return nil }
func (f *fakeStateManager) Refresh() error                                          { return nil }
func (f *fakeStateManager) FileManager() fsx.Manager                                { return nil }

// fakeHelmManager is a controllable HelmManager for tests.
type fakeHelmManager struct {
	releases []*release.Release
	err      error
}

func (f *fakeHelmManager) ListAll() ([]*release.Release, error) {
	return f.releases, f.err
}

// fakeKubeClient is a controllable KubeClient for tests.
type fakeKubeClient struct {
	pvList *unstructured.UnstructuredList
	err    error
}

func (f *fakeKubeClient) List(_ context.Context, _ kube.ResourceKind, _ string, _ kube.WaitOptions) (*unstructured.UnstructuredList, error) {
	return f.pvList, f.err
}

// ---------------------------------------------------------------------------
// Builder helpers
// ---------------------------------------------------------------------------

// newBlockNodeRelease builds a minimal Helm release for use in tests.
// If withStatefulSet is true, the manifest contains a StatefulSet labelled
// app.kubernetes.io/instance=block-node so the BlockNode checker will match it.
func newBlockNodeRelease(name, namespace, appVersion, chartVersion string, withStatefulSet bool) *release.Release {
	manifest := ""
	if withStatefulSet {
		manifest = `
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: block-node-server
  namespace: ` + namespace + `
  labels:
    app.kubernetes.io/instance: block-node
`
	}
	return &release.Release{
		Name:      name,
		Namespace: namespace,
		Info: &release.Info{
			Status:        release.StatusDeployed,
			FirstDeployed: helmtime.Now(),
			LastDeployed:  helmtime.Now(),
		},
		Chart: &chart.Chart{
			Metadata: &chart.Metadata{
				Name:       "block-node-server",
				Version:    chartVersion,
				AppVersion: appVersion,
			},
		},
		Manifest: manifest,
	}
}

// newPV builds a minimal PersistentVolume unstructured object for use in tests.
func newPV(name, claimNamespace, claimName, size, hostPath string) unstructured.Unstructured {
	return unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{"name": name},
			"spec": map[string]interface{}{
				"claimRef": map[string]interface{}{
					"namespace": claimNamespace,
					"name":      claimName,
				},
				"capacity": map[string]interface{}{
					"storage": size,
				},
				"hostPath": map[string]interface{}{
					"path": hostPath,
				},
			},
		},
	}
}
