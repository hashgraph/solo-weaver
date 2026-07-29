// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package reality

import (
	"context"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func makePV(claimNamespace, claimName, size, hostPath string) unstructured.Unstructured {
	return unstructured.Unstructured{
		Object: map[string]interface{}{
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

func TestPopulateStorageFromPVs_ApplicationStatePVC(t *testing.T) {
	pvs := &unstructured.UnstructuredList{
		Items: []unstructured.Unstructured{
			makePV("test-ns", "application-state-storage-pvc", "50Gi", "/mnt/fast-storage/block-node/application-state"),
		},
	}
	storage := &models.BlockNodeStorage{}
	checker := &blockNodeChecker{}

	if err := checker.populateStorageFromPVs(pvs, "test-ns", storage); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if storage.ApplicationStatePath != "/mnt/fast-storage/block-node/application-state" {
		t.Errorf("expected ApplicationStatePath '/mnt/fast-storage/block-node/application-state', got %q", storage.ApplicationStatePath)
	}
	if storage.ApplicationStateSize != "50Gi" {
		t.Errorf("expected ApplicationStateSize '50Gi', got %q", storage.ApplicationStateSize)
	}
}

func TestPopulateStorageFromPVs_AllFields(t *testing.T) {
	pvs := &unstructured.UnstructuredList{
		Items: []unstructured.Unstructured{
			makePV("ns", "live-storage-pvc", "100Gi", "/mnt/live"),
			makePV("ns", "archive-storage-pvc", "200Gi", "/mnt/archive"),
			makePV("ns", "log-storage-pvc", "10Gi", "/mnt/log"),
			makePV("ns", "verification-storage-pvc", "20Gi", "/mnt/verification"),
			makePV("ns", "plugins-storage-pvc", "5Gi", "/mnt/plugins"),
			makePV("ns", "application-state-storage-pvc", "50Gi", "/mnt/app-state"),
		},
	}
	storage := &models.BlockNodeStorage{}
	checker := &blockNodeChecker{}

	if err := checker.populateStorageFromPVs(pvs, "ns", storage); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if storage.LivePath != "/mnt/live" {
		t.Errorf("LivePath: got %q", storage.LivePath)
	}
	if storage.ArchivePath != "/mnt/archive" {
		t.Errorf("ArchivePath: got %q", storage.ArchivePath)
	}
	if storage.LogPath != "/mnt/log" {
		t.Errorf("LogPath: got %q", storage.LogPath)
	}
	if storage.VerificationPath != "/mnt/verification" {
		t.Errorf("VerificationPath: got %q", storage.VerificationPath)
	}
	if storage.PluginsPath != "/mnt/plugins" {
		t.Errorf("PluginsPath: got %q", storage.PluginsPath)
	}
	if storage.ApplicationStatePath != "/mnt/app-state" {
		t.Errorf("ApplicationStatePath: got %q", storage.ApplicationStatePath)
	}
	if storage.ApplicationStateSize != "50Gi" {
		t.Errorf("ApplicationStateSize: got %q", storage.ApplicationStateSize)
	}
}

func TestPopulateStorageFromPVs_SkipsDifferentNamespace(t *testing.T) {
	pvs := &unstructured.UnstructuredList{
		Items: []unstructured.Unstructured{
			makePV("other-ns", "application-state-storage-pvc", "50Gi", "/mnt/app-state"),
		},
	}
	storage := &models.BlockNodeStorage{}
	checker := &blockNodeChecker{}

	if err := checker.populateStorageFromPVs(pvs, "test-ns", storage); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if storage.ApplicationStatePath != "" {
		t.Errorf("expected empty ApplicationStatePath for wrong namespace, got %q", storage.ApplicationStatePath)
	}
}

func TestPopulateStorageFromPVs_NilInputsNoError(t *testing.T) {
	checker := &blockNodeChecker{}

	if err := checker.populateStorageFromPVs(nil, "ns", nil); err != nil {
		t.Fatalf("unexpected error for nil inputs: %v", err)
	}
}

// fakeStateManager satisfies state.Manager by embedding the interface (nil) and
// overriding only State(), the single method RefreshState calls. Any other call
// would panic, which keeps the fake honest about what the checker actually uses.
type fakeStateManager struct {
	state.Manager
	st state.State
}

func (f fakeStateManager) State() state.State { return f.st }

// fakeHelmManager returns a fixed release list from ListAll.
type fakeHelmManager struct {
	releases []*release.Release
}

func (f fakeHelmManager) ListAll() ([]*release.Release, error) { return f.releases, nil }

// fakeKubeClient returns an empty PV list so populateStorageFromPVs is a no-op.
type fakeKubeClient struct{}

func (f fakeKubeClient) List(_ context.Context, _ kube.ResourceKind, _ string, _ kube.WaitOptions) (*unstructured.UnstructuredList, error) {
	return &unstructured.UnstructuredList{}, nil
}

// TestRefreshState_PreservesTrafficShapingDisabled verifies that the weaver-only
// install decision (which cannot be recovered from the Helm release or cluster)
// survives a reality refresh that rebuilds BlockNodeState from a found release.
// Without preservation, the rebuild resets it to the enabled default (false),
// which wrongly makes reconfigure/upgrade re-provision tc shaping and attempt a
// daemon install for a block node deliberately installed without it.
func TestRefreshState_PreservesTrafficShapingDisabled(t *testing.T) {
	const manifest = `apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: block-node-block-node-server
  namespace: block-node
  labels:
    app.kubernetes.io/instance: block-node
`
	re := &release.Release{
		Name:      "block-node",
		Namespace: "block-node",
		Info:      &release.Info{Status: release.StatusDeployed},
		Chart: &chart.Chart{
			Metadata: &chart.Metadata{Name: "block-node-server", Version: "0.28.0", AppVersion: "0.28.0"},
		},
		Manifest: manifest,
	}

	persisted := state.NewBlockNodeState()
	persisted.ReleaseInfo.Status = release.StatusDeployed
	persisted.TrafficShapingDisabled = true

	full := state.State{}
	full.BlockNodeState = persisted

	checker := &blockNodeChecker{
		sm:            fakeStateManager{st: full},
		newHelm:       func() (HelmManager, error) { return fakeHelmManager{releases: []*release.Release{re}}, nil },
		newKube:       func() (KubeClient, error) { return fakeKubeClient{}, nil },
		clusterExists: func() (bool, error) { return true, nil },
	}

	got, err := checker.RefreshState(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ReleaseInfo.Name != "block-node" {
		t.Fatalf("expected release to be found and rebuilt, got name %q", got.ReleaseInfo.Name)
	}
	if !got.TrafficShapingDisabled {
		t.Error("TrafficShapingDisabled must be preserved as true across a reality refresh, got false")
	}
}
