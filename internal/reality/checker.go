// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/hardware"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/software"

	"github.com/hashgraph/solo-weaver/internal/kube"
	helm2 "github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
	htime "helm.sh/helm/v3/pkg/time"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// Checker is the abstraction for accessing the current state of the system
// including cluster, machines, and blocknodes etc.
//
// It is used by the BLL to make decisions based on the actual state of the system.
// It is separate from the StateManager and BLL which manages the current state and executes intents.
//
// This separation allows for a clearer distinction between current and actual states as well as intent execution.
// This is supposed to be resource-heavy and may involve network calls, so it should be used judiciously.
type Checker interface {
	ClusterState(ctx context.Context) (state.ClusterState, error)

	MachineState(ctx context.Context) (state.MachineState, error)

	BlockNodeState(ctx context.Context) (state.BlockNodeState, error)
}

type realityChecker struct {
	current       state.State
	sandboxBinDir string // overrideable for tests; defaults to models.Paths().SandboxBinDir
	stateDir      string // overrideable for tests; defaults to models.Paths().StateDir
}

func (r *realityChecker) ClusterState(ctx context.Context) (state.ClusterState, error) {
	cs := state.NewClusterState()

	// Fast pre-check: verify the cluster is reachable before making a
	// full API call.  ClusterExists() uses a short local check (kubeconfig
	// presence + a cheap /version ping with a tight deadline) so we don't
	// block for 32 s when there is no cluster.
	exists, err := kube.ClusterExists()
	if !exists {
		logx.As().Debug().Err(err).Msg("Kubernetes cluster does not exist or is unreachable, returning empty ClusterState")
		return cs, nil
	}

	clusterInfo, err := kube.RetrieveClusterInfo()
	if err != nil {
		logx.As().Error().Err(err).Msg("Failed to retrieve cluster info, returning empty ClusterState")
		return cs, nil // return empty state if we fail to get cluster info, since the cluster might not exist
	}

	cs.Initialize(clusterInfo)

	return cs, nil
}

func (r *realityChecker) MachineState(ctx context.Context) (state.MachineState, error) {
	now := htime.Now()
	ms := state.NewMachineState()

	ms.Software = r.refreshSoftwareState()
	ms.Hardware = r.refreshHardwareState()

	ms.LastSync = now
	return ms, nil
}

// refreshSoftwareState probes each known binary on the filesystem and merges
// with persisted version/configured metadata from current state.
//
// Source-of-truth priority (highest → lowest):
//  1. New state.yaml MachineState.Software map  (set by new DefaultStateManager)
//  2. Legacy sidecar files  <StateDir>/<name>.installed / .configured
//     (set by the legacy state.Manager used by all installers today)
//  3. Binary presence on disk  (live filesystem stat)
func (r *realityChecker) refreshSoftwareState() map[string]state.SoftwareState {
	result := make(map[string]state.SoftwareState)

	sandboxBinDir := r.sandboxBinDir
	if sandboxBinDir == "" {
		sandboxBinDir = models.Paths().SandboxBinDir
	}
	stateDir := r.stateDir
	if stateDir == "" {
		stateDir = models.Paths().StateDir
	}

	for _, name := range software.KnownSoftwareNames() {
		sw := state.SoftwareState{Name: name}

		// --- Priority 1: carry from new MachineState.Software map ---
		if persisted, ok := r.current.MachineState.Software[name]; ok && persisted.Name != "" {
			sw = persisted
		} else {
			// --- Priority 2: read legacy sidecar files ---
			sw.Installed, sw.Version = readLegacySidecarState(stateDir, name, "installed")
			if configured, _ := readLegacySidecarState(stateDir, name, "configured"); configured {
				sw.Configured = true
			}
		}

		// --- Priority 3: live binary check always overrides Installed ---
		// If binary is absent on disk, force Installed=false regardless of what
		// the state files say (handles manual deletions).
		binPath := filepath.Join(sandboxBinDir, name)
		if _, err := os.Stat(binPath); err == nil {
			sw.Installed = true
		} else {
			sw.Installed = false
			logx.As().Debug().
				Str("binary", binPath).
				Msg("Binary not found on filesystem — marking as not installed")
		}

		sw.LastSync = htime.Now()
		result[name] = sw
	}

	return result
}

// readLegacySidecarState reads a legacy <name>.<stateType> sidecar file from stateDir.
// It returns (true, version) when the file exists, (false, "") otherwise.
// The file content is expected to be in the format written by state.Manager.RecordState:
//
//	"installed at version 1.30.0\n"
//	"configured at version 1.30.0\n"
func readLegacySidecarState(stateDir, name, stateType string) (exists bool, version string) {
	filePath := filepath.Join(stateDir, name+"."+stateType)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return false, ""
	}

	// Parse "installed at version 1.30.0"
	line := strings.TrimSpace(string(data))
	const marker = " at version "
	if idx := strings.Index(line, marker); idx != -1 {
		version = strings.TrimSpace(line[idx+len(marker):])
	}

	return true, version
}

// refreshHardwareState collects current host hardware metrics.
func (r *realityChecker) refreshHardwareState() map[string]state.HardwareState {
	result := make(map[string]state.HardwareState)
	now := htime.Now()

	hp := hardware.GetHostProfile()

	result["os"] = state.HardwareState{
		Type:     "os",
		Info:     fmt.Sprintf("%s %s", hp.GetOSVendor(), hp.GetOSVersion()),
		LastSync: now,
	}

	result["cpu"] = state.HardwareState{
		Type:     "cpu",
		Count:    int(hp.GetCPUCores()),
		LastSync: now,
	}

	result["memory"] = state.HardwareState{
		Type:     "memory",
		Size:     fmt.Sprintf("%d GB", hp.GetTotalMemoryGB()),
		Info:     fmt.Sprintf("%d GB available", hp.GetAvailableMemoryGB()),
		LastSync: now,
	}

	result["storage"] = state.HardwareState{
		Type:     "storage",
		Size:     fmt.Sprintf("%d GB", hp.GetTotalStorageGB()),
		LastSync: now,
	}

	if ssd := hp.GetSSDStorageGB(); ssd > 0 {
		result["storage-ssd"] = state.HardwareState{
			Type:     "storage-ssd",
			Size:     fmt.Sprintf("%d GB", ssd),
			LastSync: now,
		}
	}

	if hdd := hp.GetHDDStorageGB(); hdd > 0 {
		result["storage-hdd"] = state.HardwareState{
			Type:     "storage-hdd",
			Size:     fmt.Sprintf("%d GB", hdd),
			LastSync: now,
		}
	}

	return result
}

func (r *realityChecker) BlockNodeState(ctx context.Context) (state.BlockNodeState, error) {
	now := htime.Now()
	bn := state.NewBlockNodeState()

	if exists, err := kube.ClusterExists(); !exists {
		logx.As().Debug().Err(err).Msg("Kubernetes cluster does not exist, skipping BlockNodeState state check")
		return bn, nil // the cluster does not exist, so nothing to check
	}

	logx.As().Info().Msg("Refreshing BlockNodeState state from Kubernetes cluster")
	re, err := r.findBlockNodeHelmRelease()
	if err != nil {
		return bn, err
	}

	if re == nil {
		return bn, nil // no BlockNodeState release found
	}

	bn = state.BlockNodeState{
		ReleaseInfo: state.HelmReleaseInfo{
			Name:          re.Name,
			Version:       re.Chart.Metadata.AppVersion,
			Namespace:     re.Namespace,
			ChartName:     re.Chart.ChartFullPath(),
			ChartVersion:  re.Chart.Metadata.Version,
			FirstDeployed: re.Info.FirstDeployed,
			LastDeployed:  re.Info.LastDeployed,
			Deleted:       re.Info.Deleted,
			Status:        re.Info.Status,
		},
		Storage:  models.BlockNodeStorage{},
		LastSync: now,
	}

	// get storage info from from PVs and PVCs associated with the BlockNodeState release
	k8s, err := kube.NewClient()
	if err != nil {
		return bn, err
	}

	// PersistentVolumes are cluster-scoped -> pass empty namespace
	pvs, err := k8s.List(ctx, kube.KindPV, "", kube.WaitOptions{})
	if err != nil {
		return bn, err
	}

	// loop through pvs, find those associated with the BlockNodeState release
	for _, pv := range pvs.Items {
		logx.As().Debug().Any("pv", pv).Msg("Checking PV for BlockNodeState storage")
		claimRef, found, err := unstructured.NestedMap(pv.Object, "spec", "claimRef")
		if err != nil || !found {
			continue
		}
		claimNamespace, found, err := unstructured.NestedString(claimRef, "namespace")
		if err != nil || !found {
			continue
		}
		claimName, found, err := unstructured.NestedString(claimRef, "name")
		if err != nil || !found {
			continue
		}

		// check if this PVC belongs to the BlockNodeState release
		if claimNamespace == re.Namespace {
			size, found, err := unstructured.NestedString(pv.Object, "spec", "capacity", "storage")
			if err != nil || !found {
				return bn, errorx.IllegalState.New("PV %s does not have a storage size", pv.GetName())
			}

			hostpath, found, err := unstructured.NestedString(pv.Object, "spec", "hostPath", "path")
			if err != nil || !found {
				return bn, errorx.IllegalState.New("PV %s does not have a hostPath", pv.GetName())
			}

			logx.As().Debug().
				Str("pv", pv.GetName()).
				Str("pvc", claimName).
				Str("namespace", claimNamespace).
				Str("size", size).
				Str("hostPath", hostpath).
				Msg("Found BlockNodeState storage PV/PVC")

			if strings.Contains(claimName, "live") {
				bn.Storage.LivePath = hostpath
				bn.Storage.LiveSize = size
			} else if strings.Contains(claimName, "archive") {
				bn.Storage.ArchivePath = hostpath
				bn.Storage.ArchiveSize = size
			} else if strings.Contains(claimName, "log") {
				bn.Storage.LogPath = hostpath
				bn.Storage.LogSize = size
			} else if strings.Contains(claimName, "verification") {
				bn.Storage.VerificationPath = hostpath
				bn.Storage.VerificationSize = size
			}
		}
	}

	r.current.BlockNodeState = bn
	logx.As().Debug().Any("state", bn).Msg("Refreshed block node state")

	return bn, nil
}

func (r *realityChecker) findBlockNodeHelmRelease() (*release.Release, error) {
	helm, err := helm2.NewManager()
	if err != nil {
		return nil, err
	}

	releases, err := helm.ListAll()
	if err != nil {
		return nil, err
	}

	for _, re := range releases {
		logx.As().Debug().Any("release", re.Info).Msg("Found Helm release: " + re.Name)
		manifests, err := UnmarshalManifest(re.Manifest)
		if err != nil {
			return nil, err
		}
		for _, manifest := range manifests {
			if manifest.GetKind() == "StatefulSet" {
				for l, v := range manifest.GetLabels() {
					if l == "app.kubernetes.io/instance" && v == "block-node" {
						logx.As().Debug().
							Str("releaseName", re.Name).
							Str("namespace", manifest.GetNamespace()).
							Str("name", manifest.GetName()).
							Any("labels", manifest.GetLabels()).
							Msg("Found BlockNodeState StatefulSet in Helm release")
						return re, nil
					}
				}
			}
		}
	}

	return nil, nil // no BlockNodeState release found
}

// UnmarshalManifest parses a Helm release manifest (possibly multi-doc YAML)
// and returns a slice of Unstructured objects (one per non-empty document).
func UnmarshalManifest(manifest string) ([]*unstructured.Unstructured, error) {
	dec := yaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096)
	var out []*unstructured.Unstructured

	for {
		// decode into a generic map first
		var doc map[string]interface{}
		if err := dec.Decode(&doc); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		// skip empty documents (e.g. separators or whitespace)
		if len(doc) == 0 {
			continue
		}
		u := &unstructured.Unstructured{Object: doc}
		out = append(out, u)
	}
	return out, nil
}

func NewChecker(current state.State) (Checker, error) {
	return &realityChecker{current: current}, nil
}
