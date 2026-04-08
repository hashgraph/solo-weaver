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

const ModelVersion = "v2" // State model version (from v0.14.0) — increment on breaking changes requiring migration
const StateFileName = "state.yaml"

// State is the on-disk envelope.  Envelope fields (Hash, HashAlgo, StateFile,
// LastSync) are bookkeeping metadata that never participate in hashing.
//
// StateRecord is serialised as a nested "state" object so the envelope/content
// split is immediately visible to anyone reading the YAML or JSON file:
//
//	hash: "e3b0c4..."
//	hashAlgo: sha256
//	stateFile: /opt/solo/weaver/state/state.yaml
//	lastSync: "2026-03-17T..."
//	state:
//	  version: v2
//	  machineState: ...
//	  clusterState: ...
//	  blockNodeState: ...
//
// Go field promotion still applies — all StateRecord fields (Version,
// MachineState, …) are accessible directly as state.Version, state.MachineState,
// etc. without going through state.StateRecord.
type State struct {
	// Envelope — excluded from hash; never add domain data here
	Hash      string     `yaml:"hash,omitempty"     json:"hash,omitempty"`     // digest of StateRecord
	HashAlgo  string     `yaml:"hashAlgo,omitempty" json:"hashAlgo,omitempty"` // algorithm used for Hash (e.g. "sha256")
	StateFile string     `yaml:"stateFile"          json:"stateFile"`          // path to the state file on disk
	LastSync  htime.Time `yaml:"lastSync,omitempty" json:"lastSync,omitempty"` // last time state was flushed to disk

	// Domain content — serialised as a nested "state" object for readability.
	// The named YAML/JSON tag does not affect Go field promotion: state.Version,
	// state.MachineState, etc. all still resolve directly.
	StateRecord `yaml:"state" json:"state"`
}

// StateRecord holds all provisioning domain data — the fields that participate
// in the content hash. Add new domain fields here, never directly to State.
//
// Envelope fields (Hash, HashAlgo, StateFile, LastSync) live in State and are
// intentionally excluded from hashing because they are bookkeeping metadata,
// not provisioning state.
type StateRecord struct {
	Version          string          `yaml:"version" json:"version"`
	ProvisionerState ProvisionerInfo `yaml:"provisioner" json:"provisioner"`
	MachineState     MachineState    `yaml:"machineState" json:"machineState"`
	ClusterState     ClusterState    `yaml:"clusterState" json:"clusterState"`
	BlockNodeState   BlockNodeState  `yaml:"blockNodeState" json:"blockNodeState"`
	TeleportState    TeleportState   `yaml:"teleportState" json:"teleportState"`
	LastAction       ActionHistory   `yaml:"lastAction,omitempty" json:"lastAction,omitempty"` // last action performed, used for tracking and debugging
}

// Hashable returns a deep copy of the domain StateRecord with all reconciliation
// timestamps (LastSync) zeroed. This is the canonical input for hashing:
//   - Envelope fields (Hash, HashAlgo, StateFile, LastSync on State) are already
//     excluded by returning only StateRecord.
//   - Sub-state LastSync fields are zeroed because they advance on every
//     reconciliation cycle and do not represent meaningful provisioning changes.
//
// Note: We don't want to use pointer receiver here because we don't need to modify the original state instance.
func (s State) Hashable() StateRecord {
	r := s.StateRecord

	// Zero sub-state reconciliation timestamps
	r.MachineState.LastSync = htime.Time{}
	r.ClusterState.LastSync = htime.Time{}
	r.BlockNodeState.LastSync = htime.Time{}
	r.TeleportState.LastSync = htime.Time{}

	// Software map — copy before mutation to avoid aliasing the original
	if len(r.MachineState.Software) > 0 {
		sw := make(map[string]SoftwareState, len(r.MachineState.Software))
		for k, v := range r.MachineState.Software {
			v.LastSync = htime.Time{}
			sw[k] = v
		}
		r.MachineState.Software = sw
	}

	// Hardware map — same copy-before-mutation pattern
	if len(r.MachineState.Hardware) > 0 {
		hw := make(map[string]HardwareState, len(r.MachineState.Hardware))
		for k, v := range r.MachineState.Hardware {
			v.LastSync = htime.Time{}
			hw[k] = v
		}
		r.MachineState.Hardware = hw
	}

	return r
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
	Name       string           `yaml:"name" json:"name"`
	Version    string           `yaml:"version" json:"version"`
	Installed  bool             `yaml:"installed" json:"installed"`
	Configured bool             `yaml:"configured" json:"configured"`
	Metadata   models.StringMap `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	LastSync   htime.Time       `yaml:"lastSync,omitempty" json:"lastSync,omitempty"` // last time state was reconciled
}

type HardwareState struct {
	Type     string           `yaml:"type" json:"type"`                       // e.g. "CPU", "RAM", "Disk"
	Info     string           `yaml:"info" json:"info"`                       // e.g. "Intel i7", "16GB", "1TB SSD"
	Count    int              `yaml:"count,omitempty" json:"count,omitempty"` // e.g. number of CPUs
	Size     string           `yaml:"size,omitempty" json:"size,omitempty"`   // e.g. size for RAM or Disk
	Metadata models.StringMap `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	LastSync htime.Time       `yaml:"lastSync,omitempty" json:"lastSync,omitempty"` // last time state was reconciled
}

type BlockNodeState struct {
	ReleaseInfo HelmReleaseInfo         `yaml:",inline" json:",inline"`
	Storage     models.BlockNodeStorage `yaml:"storage" json:"storage"`
	LastSync    htime.Time              `yaml:"lastSync,omitempty" json:"lastSync,omitempty"` // last time state was reconciled
}

// ClusterNodeState represents a single Kubernetes node summary.
type ClusterNodeState struct {
	Name        string           `yaml:"name" json:"name"`
	Role        string           `yaml:"role" json:"role"` // e.g. "master", "worker"
	Ready       bool             `yaml:"ready" json:"ready"`
	KubeletVer  string           `yaml:"kubeletVersion" json:"kubeletVersion"`
	Labels      models.StringMap `yaml:"labels,omitempty" json:"labels,omitempty"`
	Annotations models.StringMap `yaml:"annotations,omitempty" json:"annotations,omitempty"`
	LastSync    htime.Time       `yaml:"lastSync,omitempty" json:"lastSync,omitempty"` // last time state was reconciled
}

// HelmReleaseInfo captures minimal status for a Helm release managed by the system.
//
// Breaking change introduced in ModelVersion v2 (released with v0.14.0):
//   - yaml:"version"   now stores the Helm chart version (was app version in v1).
//   - yaml:"appVersion" is new; stores the app version (was yaml:"version" in v1).
//   - yaml:"deletedAt" renamed from yaml:"deleted" in v1.
//   - yaml:"status"    gained an explicit YAML tag (was JSON-only in v1).
type HelmReleaseInfo struct {
	Name         string         `yaml:"name"             json:"name"`
	ChartVersion string         `yaml:"version"          json:"version"`    // Helm chart version; yaml key "version" preserved for on-disk compatibility
	AppVersion   string         `yaml:"appVersion"       json:"appVersion"` // version of the application packaged inside the chart (Metadata.AppVersion)
	Namespace    string         `yaml:"namespace"        json:"namespace"`
	ChartRef     string         `yaml:"chartRef"         json:"chartRef"` // e.g. "oci://ghcr.io/hedera/solo-weaver-block-node" needs to match deployed chart
	ChartName    string         `yaml:"chartName"        json:"chartName"`
	Status       release.Status `yaml:"status,omitempty" json:"status,omitempty"` // current state of the release
	// FirstDeployed is when the release was first deployed.
	FirstDeployed htime.Time `yaml:"firstDeployed,omitempty" json:"first_deployed,omitempty"`
	// LastDeployed is when the release was last deployed.
	LastDeployed htime.Time `yaml:"lastDeployed,omitempty" json:"last_deployed,omitempty"`
	// DeletedAt tracks when this release was deleted.
	DeletedAt htime.Time `yaml:"deletedAt,omitempty" json:"deletedAt,omitempty"`
}

// ClusterState represents the persisted state of a Kubernetes cluster.
type ClusterState struct {
	models.ClusterInfo
	Created  bool       `yaml:"created" json:"created"`                       // whether the cluster was created by the provisioner
	LastSync htime.Time `yaml:"lastSync,omitempty" json:"lastSync,omitempty"` // last time state was reconciled
}

// TeleportState represents the persisted state of Teleport agents.
type TeleportState struct {
	NodeAgent    TeleportNodeAgentState    `yaml:"nodeAgent" json:"nodeAgent"`
	ClusterAgent TeleportClusterAgentState `yaml:"clusterAgent" json:"clusterAgent"`
	LastSync     htime.Time                `yaml:"lastSync,omitempty" json:"lastSync,omitempty"`
}

// TeleportNodeAgentState represents the persisted state of the Teleport node agent (binary-based).
type TeleportNodeAgentState struct {
	Installed  bool   `yaml:"installed" json:"installed"`
	Configured bool   `yaml:"configured" json:"configured"`
	Version    string `yaml:"version,omitempty" json:"version,omitempty"`
}

// TeleportClusterAgentState represents the persisted state of the Teleport cluster agent (Helm-based).
type TeleportClusterAgentState struct {
	Installed    bool   `yaml:"installed" json:"installed"`
	Release      string `yaml:"release,omitempty" json:"release,omitempty"`
	Namespace    string `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	ChartVersion string `yaml:"chartVersion,omitempty" json:"chartVersion,omitempty"`
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
func NewState(stateFile string) State {
	p := models.Paths().Clone()

	// set provisioner state based on current version and executable path, this is informational only and does not impact any functionality
	exePath, err := os.Executable()
	if err != nil {
		logx.As().Warn().Err(err).Msg("Failed to get executable path for provisioner state, using empty string")
	}

	if stateFile == "" {
		stateFile = path.Join(p.StateDir, StateFileName)
	}

	versionInfo := version.Get()
	return State{
		// Envelope fields
		StateFile: stateFile,
		LastSync:  htime.Time{},
		// Domain content
		StateRecord: StateRecord{
			Version: ModelVersion,
			ProvisionerState: ProvisionerInfo{
				Version:    versionInfo.Number,
				Commit:     versionInfo.Commit,
				GoVersion:  versionInfo.GoVersion,
				Executable: exePath,
			},
			MachineState:   NewMachineState(),
			ClusterState:   NewClusterState(),
			BlockNodeState: NewBlockNodeState(),
		},
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
