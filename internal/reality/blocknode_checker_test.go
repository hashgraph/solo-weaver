// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package reality

import (
	"context"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
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

// fakeKubeClient returns pvs from List; with none set, populateStorageFromPVs is a no-op.
type fakeKubeClient struct {
	pvs []unstructured.Unstructured
}

func (f fakeKubeClient) List(_ context.Context, _ kube.ResourceKind, _ string, _ kube.WaitOptions) (*unstructured.UnstructuredList, error) {
	return &unstructured.UnstructuredList{Items: f.pvs}, nil
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

// TestRefreshState_PreservesShaping verifies that the persisted traffic-shaping
// content (egress NIC, link rate, per-class overrides) — which, like
// TrafficShapingDisabled, cannot be recovered from the Helm release or the live
// cluster — survives a reality refresh that rebuilds BlockNodeState from a found
// release. Without preservation, upgrade/reconfigure would auto-detect the NIC and
// drop the operator's original rate/overrides instead of re-asserting them.
func TestRefreshState_PreservesShaping(t *testing.T) {
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

	prio := 0
	persisted := state.NewBlockNodeState()
	persisted.ReleaseInfo.Status = release.StatusDeployed
	persisted.Shaping = &state.ShapingState{
		EgressInterface: "eth0",
		LinkRate:        "1gbit",
		ShapeOverrides: map[string]models.ShapeOverride{
			"publisher": {Rate: "800mbit", Ceil: "1gbit", Prio: &prio},
		},
	}

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
	if got.Shaping == nil {
		t.Fatal("Shaping must be preserved across a reality refresh, got nil")
	}
	if got.Shaping.EgressInterface != "eth0" || got.Shaping.LinkRate != "1gbit" {
		t.Errorf("Shaping NIC/rate not preserved: got %q/%q, want eth0/1gbit",
			got.Shaping.EgressInterface, got.Shaping.LinkRate)
	}
	if ov, ok := got.Shaping.ShapeOverrides["publisher"]; !ok || ov.Rate != "800mbit" {
		t.Errorf("Shaping overrides not preserved: got %+v", got.Shaping.ShapeOverrides)
	}
}

// TestRefreshState_PreservesChartRef verifies the chart reference survives a
// reality refresh. Helm does not record it in release metadata, so rebuilding
// BlockNodeState from a found release loses it; patchBlockNodeState re-injects it
// on flush, which is after the workflow has already run.
//
// The gap between those two points is what makes this matter. Anything reading
// ReleaseInfo.ChartRef off a freshly refreshed state sees an empty ref and
// concludes the deployed release has no chart — which silently disables the
// upgrade handler's chart-switch warning and the reconfigure handler's
// chart-change guard, letting a config file move a running release onto a
// different chart and then persist that chart on the way out.
func TestRefreshState_PreservesChartRef(t *testing.T) {
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
	persisted.ReleaseInfo.ChartRef = "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server"

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
	if got.ReleaseInfo.ChartRef != persisted.ReleaseInfo.ChartRef {
		t.Errorf("ChartRef must be preserved across a reality refresh: want %q, got %q",
			persisted.ReleaseInfo.ChartRef, got.ReleaseInfo.ChartRef)
	}
}

const blockNodeManifest = `apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: block-node-block-node-server
  namespace: block-node
  labels:
    app.kubernetes.io/instance: block-node
`

// deployedRelease is a deployed block-node release installed with values.
func deployedRelease(values map[string]any) *release.Release {
	return &release.Release{
		Name:      "block-node",
		Namespace: "block-node",
		Info:      &release.Info{Status: release.StatusDeployed},
		Chart: &chart.Chart{
			Metadata: &chart.Metadata{Name: "block-node-server", Version: "0.28.0", AppVersion: "0.28.0"},
		},
		Config:   values,
		Manifest: blockNodeManifest,
	}
}

// refresh runs RefreshState against persisted, a cluster holding re and pvs.
func refresh(t *testing.T, persisted state.BlockNodeState, re *release.Release, pvs ...unstructured.Unstructured) state.BlockNodeState {
	t.Helper()
	full := state.State{}
	full.BlockNodeState = persisted
	checker := &blockNodeChecker{
		sm:            fakeStateManager{st: full},
		newHelm:       func() (HelmManager, error) { return fakeHelmManager{releases: []*release.Release{re}}, nil },
		newKube:       func() (KubeClient, error) { return fakeKubeClient{pvs: pvs}, nil },
		clusterExists: func() (bool, error) { return true, nil },
	}
	got, err := checker.RefreshState(context.Background())
	require.NoError(t, err)
	return got
}

// The fields Diff marks Observable are exactly the ones the checker reads back
// from the cluster: a release and PVs carrying every value the
// checker reads make each observable field, and nothing else, differ from a
// persisted state that records none of them.
func TestRefreshState_ReadsEveryObservableField(t *testing.T) {
	re := deployedRelease(map[string]any{
		"blockNode": map[string]any{"config": map[string]any{
			"FILES_HISTORIC_BLOCK_RETENTION_THRESHOLD": "1000",
			"FILES_RECENT_BLOCK_RETENTION_THRESHOLD":   "100",
		}},
		"service": map[string]any{"annotations": map[string]any{
			"metallb.io/address-pool": "public-address-pool",
		}},
	})
	pvs := []unstructured.Unstructured{
		makePV("block-node", "live-storage-pvc", "100Gi", "/mnt/live"),
		makePV("block-node", "archive-storage-pvc", "200Gi", "/mnt/archive"),
		makePV("block-node", "log-storage-pvc", "10Gi", "/mnt/log"),
		makePV("block-node", "verification-storage-pvc", "20Gi", "/mnt/verification"),
		makePV("block-node", "plugins-storage-pvc", "5Gi", "/mnt/plugins"),
		makePV("block-node", "application-state-storage-pvc", "50Gi", "/mnt/app-state"),
	}
	// Known exposure values on the persisted side, opposite to the release's,
	// since an unknown one is never compared.
	noPool := false
	persisted := state.BlockNodeState{ServiceTopology: state.ServiceTopologySplit, MetalLBPool: &noPool}

	got := refresh(t, persisted, re, pvs...)

	var fields []string
	for _, d := range persisted.Diff(got) {
		fields = append(fields, d.Field)
	}
	require.ElementsMatch(t, state.BlockNodeObservableFields(), fields)
}

func TestRefreshState_ReadsServiceExposure(t *testing.T) {
	pool := map[string]any{"metallb.io/address-pool": "public-address-pool"}
	cases := []struct {
		name     string
		values   map[string]any
		topology string
		pool     bool
	}{
		{
			name:     "pool annotation on the main service",
			values:   map[string]any{"service": map[string]any{"type": "LoadBalancer", "annotations": pool}},
			topology: state.ServiceTopologySingle,
			pool:     true,
		},
		{
			name:     "no pool annotation",
			values:   map[string]any{"service": map[string]any{"type": "LoadBalancer"}},
			topology: state.ServiceTopologySingle,
		},
		{
			name:     "pool annotation set to null",
			values:   map[string]any{"service": map[string]any{"annotations": map[string]any{"metallb.io/address-pool": nil}}},
			topology: state.ServiceTopologySingle,
		},
		{
			name:     "pool annotation set to an empty string",
			values:   map[string]any{"service": map[string]any{"annotations": map[string]any{"metallb.io/address-pool": ""}}},
			topology: state.ServiceTopologySingle,
		},
		{
			name:     "no values",
			values:   nil,
			topology: state.ServiceTopologySingle,
		},
		{
			name:     "split topology with a pool on the external service",
			values:   map[string]any{"loadBalancer": map[string]any{"enabled": true, "annotations": pool}},
			topology: state.ServiceTopologySplit,
			pool:     true,
		},
		{
			name: "split topology enabled as a quoted string, pool only on the main service",
			values: map[string]any{
				"loadBalancer": map[string]any{"enabled": "true"},
				"service":      map[string]any{"annotations": pool},
			},
			topology: state.ServiceTopologySplit,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := refresh(t, state.BlockNodeState{}, deployedRelease(tc.values))

			require.Equal(t, tc.topology, got.ServiceTopology)
			require.NotNil(t, got.MetalLBPool)
			require.Equal(t, tc.pool, *got.MetalLBPool)
		})
	}
}
