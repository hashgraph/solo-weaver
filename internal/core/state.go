package core

import (
	"path"

	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/version"
	"helm.sh/helm/v3/pkg/release"
	htime "helm.sh/helm/v3/pkg/time"
)

type State struct {
	Version   string         `yaml:"version" json:"version"`
	Commit    string         `yaml:"commit" json:"commit"`
	File      string         `yaml:"file" json:"file"` // path to state file
	NodeType  string         `yaml:"nodeType" json:"nodeType"`
	Paths     WeaverPaths    `yaml:"paths" json:"paths"`
	Machine   MachineState   `yaml:"machine" json:"machine"`
	Cluster   ClusterState   `yaml:"cluster" json:"cluster"`
	BlockNode BlockNodeState `yaml:"blockNode" json:"blockNode"`
	LastSync  htime.Time     `yaml:"lastSync,omitempty" json:"lastSync,omitempty"` // last time state was reconciled
}

type MachineState struct {
	Software      map[string]SoftwareState `yaml:"software" json:"software"`
	Storage       map[string]StorageState  `yaml:"storage" json:"storage"`
	Initialized   bool                     `yaml:"initialized" json:"initialized"`
	InitializedAt htime.Time               `yaml:"initializedAt,omitempty" json:"initializedAt,omitempty"`
	LastSync      htime.Time               `yaml:"lastSync,omitempty" json:"lastSync,omitempty"` // last time state was reconciled
}

type SoftwareState struct {
	Name        string     `yaml:"name" json:"name"`
	Version     string     `yaml:"version" json:"version"`
	Source      string     `yaml:"source" json:"source"`
	Installed   bool       `yaml:"installed" json:"installed"`
	InstalledAt htime.Time `yaml:"installedAt,omitempty" json:"installedAt,omitempty"`
	LastSync    htime.Time `yaml:"lastSync,omitempty" json:"lastSync,omitempty"` // last time state was reconciled
}

type StorageState struct {
	Name      string     `yaml:"name" json:"name"`
	Mounted   bool       `yaml:"mounted" json:"mounted"`
	MountedAt htime.Time `yaml:"mountedAt,omitempty" json:"mountedAt,omitempty"`
}

type BlockNodeState struct {
	ReleaseInfo HelmReleaseInfo         `yaml:",inline" json:",inline"`
	Storage     config.BlockNodeStorage `yaml:"storage" json:"storage"`
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
	Chart        string `yaml:"chart" json:"chart"`
	ChartVersion string `yaml:"chartVersion" json:"chartVersion"` // Chart version
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
	HelmReleases     map[string]HelmReleaseInfo  `yaml:"helmReleases,omitempty" json:"helmReleases,omitempty"` // keyed by release name (namespace/name)
	Labels           map[string]string           `yaml:"labels,omitempty" json:"labels,omitempty"`
	Annotations      map[string]string           `yaml:"annotations,omitempty" json:"annotations,omitempty"`
	Health           string                      `yaml:"health,omitempty" json:"health,omitempty"` // e.g. "Healthy", "Degraded", "Unknown"
	Created          bool                        `yaml:"created" json:"created"`
	CreatedAt        htime.Time                  `yaml:"createdAt,omitempty" json:"createdAt,omitempty"`
	LastSync         htime.Time                  `yaml:"lastSync,omitempty" json:"lastSync,omitempty"` // last time state was reconciled
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
		Paths: *p,

		Machine: MachineState{
			Software:    make(map[string]SoftwareState),
			Storage:     make(map[string]StorageState),
			Initialized: false,
		},

		Cluster: ClusterState{
			Nodes:        make(map[string]ClusterNodeState),
			HelmReleases: make(map[string]HelmReleaseInfo),
			Created:      false,
		},

		BlockNode: BlockNodeState{
			ReleaseInfo: HelmReleaseInfo{
				Status: release.StatusUnknown,
			},
		},
		LastSync: htime.Time{},
	}
}
