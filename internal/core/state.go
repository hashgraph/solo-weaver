package core

import (
	"path"
	"time"

	"golang.hedera.com/solo-weaver/internal/config"
	"golang.hedera.com/solo-weaver/internal/version"
)

type State struct {
	Version   string          `yaml:"version" json:"version"`
	Commit    string          `yaml:"commit" json:"commit"`
	File      string          `yaml:"file" json:"file"` // path to state file
	Paths     *WeaverPaths    `yaml:"paths" json:"paths"`
	Machine   *MachineState   `yaml:"machine" json:"machine"`
	Cluster   *ClusterState   `yaml:"cluster" json:"cluster"`
	BlockNode *BlockNodeState `yaml:"blockNode" json:"blockNode"`
	LastSync  *time.Time      `yaml:"lastSync,omitempty" json:"lastSync,omitempty"` // last time state was reconciled
}

type MachineState struct {
	Software      map[string]SoftwareState `yaml:"software" json:"software"`
	Storage       map[string]StorageState  `yaml:"storage" json:"storage"`
	Initialized   bool                     `yaml:"initialized" json:"initialized"`
	InitializedAt *time.Time               `yaml:"initializedAt,omitempty" json:"initializedAt,omitempty"`
	LastSync      *time.Time               `yaml:"lastSync,omitempty" json:"lastSync,omitempty"` // last time state was reconciled
}

type SoftwareState struct {
	Name        string     `yaml:"name" json:"name"`
	Version     string     `yaml:"version" json:"version"`
	Source      string     `yaml:"source" json:"source"`
	Installed   bool       `yaml:"installed" json:"installed"`
	InstalledAt *time.Time `yaml:"installedAt,omitempty" json:"installedAt,omitempty"`
	LastSync    *time.Time `yaml:"lastSync,omitempty" json:"lastSync,omitempty"` // last time state was reconciled
}

type StorageState struct {
	Name      string     `yaml:"name" json:"name"`
	Mounted   bool       `yaml:"mounted" json:"mounted"`
	MountedAt *time.Time `yaml:"mountedAt,omitempty" json:"mountedAt,omitempty"`
}

type BlockNodeState struct {
	Config    *config.BlockNodeConfig `yaml:"config" json:"config"`
	Created   bool                    `yaml:"installed" json:"installed"`
	CreatedAt *time.Time              `yaml:"createdAt,omitempty" json:"createdAt,omitempty"`
	LastSync  *time.Time              `yaml:"lastSync,omitempty" json:"lastSync,omitempty"` // last time state was reconciled
}

// ClusterNodeState represents a single Kubernetes node summary.
type ClusterNodeState struct {
	Name        string            `yaml:"name" json:"name"`
	Role        string            `yaml:"role" json:"role"` // e.g. "master", "worker"
	Ready       bool              `yaml:"ready" json:"ready"`
	KubeletVer  string            `yaml:"kubeletVersion" json:"kubeletVersion"`
	Labels      map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty" json:"annotations,omitempty"`
	LastSync    *time.Time        `yaml:"lastSync,omitempty" json:"lastSync,omitempty"` // last time state was reconciled
}

// HelmReleaseState captures minimal status for a Helm release managed by the system.
type HelmReleaseState struct {
	Name      string     `yaml:"name" json:"name"`
	Namespace string     `yaml:"namespace" json:"namespace"`
	Chart     string     `yaml:"chart" json:"chart"`
	Version   string     `yaml:"version" json:"version"`
	Deployed  bool       `yaml:"deployed" json:"deployed"`
	UpdatedAt *time.Time `yaml:"updatedAt,omitempty" json:"updatedAt,omitempty"`
	Notes     string     `yaml:"notes,omitempty" json:"notes,omitempty"`
	LastSync  *time.Time `yaml:"lastSync,omitempty" json:"lastSync,omitempty"` // last time state was reconciled
}

// ClusterState represents the persisted state of a Kubernetes cluster.
type ClusterState struct {
	ID               string                      `yaml:"id,omitempty" json:"id,omitempty"` // unique identifier for the cluster
	Name             string                      `yaml:"name" json:"name"`
	Provider         string                      `yaml:"provider,omitempty" json:"provider,omitempty"`   // e.g. kind, minikube, gke, eks, aks
	APIServer        string                      `yaml:"apiServer,omitempty" json:"apiServer,omitempty"` // API server endpoint
	KubeVersion      string                      `yaml:"kubeVersion,omitempty" json:"kubeVersion,omitempty"`
	KubeconfigPath   string                      `yaml:"kubeconfigPath,omitempty" json:"kubeconfigPath,omitempty"`
	Context          string                      `yaml:"context,omitempty" json:"context,omitempty"`
	DefaultNamespace string                      `yaml:"defaultNamespace,omitempty" json:"defaultNamespace,omitempty"`
	NodeCount        int                         `yaml:"nodeCount,omitempty" json:"nodeCount,omitempty"`
	Nodes            map[string]ClusterNodeState `yaml:"nodes,omitempty" json:"nodes,omitempty"` // keyed by node name
	NetworkPlugin    string                      `yaml:"networkPlugin,omitempty" json:"networkPlugin,omitempty"`
	PodCIDR          string                      `yaml:"podCIDR,omitempty" json:"podCIDR,omitempty"`
	ServiceCIDR      string                      `yaml:"serviceCIDR,omitempty" json:"serviceCIDR,omitempty"`
	StorageClasses   []string                    `yaml:"storageClasses,omitempty" json:"storageClasses,omitempty"`
	IngressEnabled   bool                        `yaml:"ingressEnabled,omitempty" json:"ingressEnabled,omitempty"`
	HelmReleases     map[string]HelmReleaseState `yaml:"helmReleases,omitempty" json:"helmReleases,omitempty"` // keyed by release name (namespace/name)
	Labels           map[string]string           `yaml:"labels,omitempty" json:"labels,omitempty"`
	Annotations      map[string]string           `yaml:"annotations,omitempty" json:"annotations,omitempty"`
	Health           string                      `yaml:"health,omitempty" json:"health,omitempty"` // e.g. "Healthy", "Degraded", "Unknown"
	Created          bool                        `yaml:"created" json:"created"`
	CreatedAt        *time.Time                  `yaml:"createdAt,omitempty" json:"createdAt,omitempty"`
	LastSync         *time.Time                  `yaml:"lastSync,omitempty" json:"lastSync,omitempty"` // last time state was reconciled
}

// NewState creates a new State instance with default values
// It does not load any persisted state from disk.
func NewState() *State {
	p := Paths().Clone()
	return &State{
		Version: version.Number(),
		Commit:  version.Commit(),

		File: path.Join(p.StateDir, "state.yaml"),

		// Version and Commit remain zero-values and can be set elsewhere (build flags).
		Paths: p,

		Machine: &MachineState{
			Software:    make(map[string]SoftwareState),
			Storage:     make(map[string]StorageState),
			Initialized: false,
		},

		Cluster: &ClusterState{
			Nodes:        make(map[string]ClusterNodeState),
			HelmReleases: make(map[string]HelmReleaseState),
			Created:      false,
		},

		BlockNode: &BlockNodeState{
			Config: &config.BlockNodeConfig{
				Storage: config.BlockNodeStorage{
					BasePath: "",
				},
			},
			Created: false,
		},
		LastSync: nil,
	}
}
