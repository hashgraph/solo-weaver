// SPDX-License-Identifier: Apache-2.0

package state

import (
	"os"
	"path"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/version"
	"helm.sh/helm/v3/pkg/release"
	htime "helm.sh/helm/v3/pkg/time"
)

const ModelVersion = "v1" // State model version — increment on breaking changes requiring migration
const StateFileName = "state.yaml"

type State struct {
	Hash     string `yaml:"hash,omitempty" json:"hash,omitempty"`         // digest of the serialized state
	HashAlgo string `yaml:"hashAlgo,omitempty" json:"hashAlgo,omitempty"` // algorithm used for Hash (e.g. "sha256")

	Version          string          `yaml:"version" json:"version"`
	StateFile        string          `yaml:"stateFile" json:"stateFile"` // path to the state file
	ProvisionerState ProvisionerInfo `yaml:"provisioner" json:"provisioner"`
	MachineState     MachineState    `yaml:"machineState" json:"machineState"`
	ClusterState     ClusterState    `yaml:"clusterState" json:"clusterState"`
	BlockNodeState   BlockNodeState  `yaml:"blockNodeState" json:"blockNodeState"`
	LastAction       ActionHistory   `yaml:"lastAction,omitempty" json:"lastAction,omitempty"` // last action performed, used for tracking and debugging
	LastSync         htime.Time      `yaml:"lastSync,omitempty" json:"lastSync,omitempty"`     // last time state was sy
}

type ActionHistory struct {
	Intent    models.Intent `yaml:"intent" json:"intent"` // e.g. "weaver block node init"
	Inputs    any           `yaml:"inputs" json:"inputs"` // inputs used for this intent; any marshallable value is accepted (e.g. models.UserInputs[T])
	Timestamp htime.Time    `yaml:"timestamp" json:"timestamp"`
}

type ProvisionerInfo struct {
	Version    string `yaml:"version" json:"version"`
	Commit     string `yaml:"commit" json:"commit"`
	GoVersion  string `yaml:"goversion" json:"goversion"`
	Executable string `yaml:"executable" json:"executable"` // location of the executable
}

type MachineState struct {
	Software map[string]SoftwareState `yaml:"software" json:"software"`
	Hardware map[string]HardwareState `yaml:"hardware" json:"hardware"`                     // e.g. CPU, RAM, Disk info
	LastSync htime.Time               `yaml:"lastSync,omitempty" json:"lastSync,omitempty"` // last time state was reconciled
}

type SoftwareState struct {
	Name       string            `yaml:"name" json:"name"`
	Version    string            `yaml:"version" json:"version"`
	Installed  bool              `yaml:"installed" json:"installed"`
	Configured bool              `yaml:"configured" json:"configured"`
	Metadata   map[string]string `yaml:"meta,omitempty" json:"meta,omitempty"`
	LastSync   htime.Time        `yaml:"lastSync,omitempty" json:"lastSync,omitempty"` // last time state was reconciled
}

type HardwareState struct {
	Type     string            `yaml:"type" json:"type"`                       // e.g. "CPU", "RAM", "Disk"
	Info     string            `yaml:"info" json:"info"`                       // e.g. "Intel i7", "16GB", "1TB SSD"
	Count    int               `yaml:"count,omitempty" json:"count,omitempty"` // e.g. number of CPUs
	Size     string            `yaml:"size,omitempty" json:"size,omitempty"`   // e.g. size for RAM or Disk
	Metadata map[string]string `yaml:"meta,omitempty" json:"meta,omitempty"`
	LastSync htime.Time        `yaml:"lastSync,omitempty" json:"lastSync,omitempty"` // last time state was reconciled
}

type BlockNodeState struct {
	ReleaseInfo HelmReleaseInfo         `yaml:",inline" json:",inline"`
	Storage     models.BlockNodeStorage `yaml:"storage" json:"storage"`
	LastSync    htime.Time              `yaml:"lastSync,omitempty" json:"lastSync,omitempty"` // last time state was reconciled
}

// ClusterNodeState represents a single Kubernetes node summary.
type ClusterNodeState struct {
	Name        string            `yaml:"name" json:"name"`
	Role        string            `yaml:"role" json:"role"` // e.g. "master", "worker"
	Ready       bool              `yaml:"ready" json:"ready"`
	KubeletVer  string            `yaml:"kubeletVersion" json:"kubeletVersion"`
	Labels      map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty" json:"annotations,omitempty"`
	LastSync    htime.Time        `yaml:"lastSync,omitempty" json:"lastSync,omitempty"` // last time state was reconciled
}

// HelmReleaseInfo captures minimal status for a Helm release managed by the system.
type HelmReleaseInfo struct {
	Name         string `yaml:"name" json:"name"`
	Version      string `yaml:"version" json:"version"` // App version
	Namespace    string `yaml:"namespace" json:"namespace"`
	ChartRef     string `yaml:"chartRef" json:"chartRef"` // e.g. "oci://ghcr.io/hedera/solo-weaver-block-node" needs to match deployed chart
	ChartName    string `yaml:"chartName" json:"chartName"`
	ChartVersion string `yaml:"chartVersion" json:"chartVersion"` // ChartName version
	// Status is the current state of the release
	Status release.Status `json:"status,omitempty"`
	// FirstDeployed is when the release was first deployed.
	FirstDeployed htime.Time `yaml:"firstDeployed,omitempty" json:"first_deployed,omitempty"`
	// LastDeployed is when the release was last deployed.
	LastDeployed htime.Time `yaml:"lastDeployed,omitempty" json:"last_deployed,omitempty"`
	// Deleted tracks when this object was deleted.
	Deleted htime.Time `yaml:"deleted" json:"deleted"`
}

// ClusterState represents the persisted state of a Kubernetes cluster.
type ClusterState struct {
	models.ClusterInfo
	Created  bool       `yaml:"created" json:"created"`                       // whether the cluster was created by the provisioner
	LastSync htime.Time `yaml:"lastSync,omitempty" json:"lastSync,omitempty"` // last time state was reconciled
}

func (cs *ClusterState) Initialize(clusterInfo *models.ClusterInfo) {
	if clusterInfo == nil {
		return
	}
	cs.ClusterInfo = *clusterInfo
	cs.Created = true
	cs.LastSync = htime.Now()
}

// NewState creates a new State instance with default values
// It does not load any persisted state from disk.
func NewState() State {
	p := models.Paths().Clone()

	// set provisioner state based on current version and executable path, this is informational only and does not impact any functionality
	exePath, err := os.Executable()
	if err != nil {
		logx.As().Warn().Err(err).Msg("Failed to get executable path for provisioner state, using empty string")
	}

	versionInfo := version.Get()
	return State{
		Version: ModelVersion,
		ProvisionerState: ProvisionerInfo{
			Version:    versionInfo.Number,
			Commit:     versionInfo.Commit,
			GoVersion:  versionInfo.GoVersion,
			Executable: exePath,
		},
		StateFile:      path.Join(p.StateDir, StateFileName),
		MachineState:   NewMachineState(),
		ClusterState:   NewClusterState(),
		BlockNodeState: NewBlockNodeState(),
		LastSync:       htime.Time{},
	}
}

func NewMachineState() MachineState {
	return MachineState{
		Software: make(map[string]SoftwareState),
		Hardware: make(map[string]HardwareState),
	}
}

func NewClusterState() ClusterState {
	return ClusterState{
		Created:  false,
		LastSync: htime.Time{},
	}
}

func NewBlockNodeState() BlockNodeState {
	return BlockNodeState{
		ReleaseInfo: HelmReleaseInfo{
			Status: release.StatusUnknown,
		},
	}
}
