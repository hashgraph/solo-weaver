package core

import (
	"time"
)

// helper to create *time.Time
func timePtr(t time.Time) *time.Time {
	return &t
}

// Clone creates a deep copy of SoftwareState
func (s *SoftwareState) Clone() SoftwareState {
	return SoftwareState{
		Name:        s.Name,
		Version:     s.Version,
		Source:      s.Source,
		Installed:   s.Installed,
		InstalledAt: s.InstalledAt,
		LastSync:    s.LastSync,
	}
}

// Clone creates a deep copy of StorageState
func (s *StorageState) Clone() StorageState {
	return StorageState{
		Name:      s.Name,
		Mounted:   s.Mounted,
		MountedAt: s.MountedAt,
	}
}

// Clone creates a deep copy of BlockNodeState
func (b *BlockNodeState) Clone() *BlockNodeState {
	if b == nil {
		return nil
	}
	clone := *b
	return &clone
}

// Clone creates a deep copy of ClusterNodeState
func (n *ClusterNodeState) Clone() ClusterNodeState {
	var labels map[string]string
	if n.Labels != nil {
		labels = make(map[string]string, len(n.Labels))
		for k, v := range n.Labels {
			labels[k] = v
		}
	}
	var ann map[string]string
	if n.Annotations != nil {
		ann = make(map[string]string, len(n.Annotations))
		for k, v := range n.Annotations {
			ann[k] = v
		}
	}
	return ClusterNodeState{
		Name:        n.Name,
		Role:        n.Role,
		Ready:       n.Ready,
		KubeletVer:  n.KubeletVer,
		Labels:      labels,
		Annotations: ann,
		LastSync:    n.LastSync,
	}
}

// Clone creates a deep copy of HelmReleaseInfo
func (h *HelmReleaseInfo) Clone() *HelmReleaseInfo {
	if h == nil {
		return nil
	}

	c := *h
	return &c
}

// Clone creates a deep copy of MachineState
func (m *MachineState) Clone() *MachineState {
	if m == nil {
		return nil
	}
	clone := &MachineState{
		Initialized:   m.Initialized,
		InitializedAt: m.InitializedAt,
		LastSync:      m.LastSync,
	}
	if m.Software != nil {
		clone.Software = make(map[string]SoftwareState, len(m.Software))
		for k, v := range m.Software {
			clone.Software[k] = v.Clone()
		}
	} else {
		clone.Software = make(map[string]SoftwareState)
	}
	if m.Storage != nil {
		clone.Storage = make(map[string]StorageState, len(m.Storage))
		for k, v := range m.Storage {
			clone.Storage[k] = v.Clone()
		}
	} else {
		clone.Storage = make(map[string]StorageState)
	}
	return clone
}

// Clone creates a deep copy of ClusterState
func (c *ClusterState) Clone() *ClusterState {
	if c == nil {
		return nil
	}
	clone := &ClusterState{
		ID:               c.ID,
		Name:             c.Name,
		Provider:         c.Provider,
		APIServer:        c.APIServer,
		KubeVersion:      c.KubeVersion,
		KubeconfigPath:   c.KubeconfigPath,
		Context:          c.Context,
		DefaultNamespace: c.DefaultNamespace,
		NodeCount:        c.NodeCount,
		NetworkPlugin:    c.NetworkPlugin,
		PodCIDR:          c.PodCIDR,
		ServiceCIDR:      c.ServiceCIDR,
		StorageClasses:   nil,
		IngressEnabled:   c.IngressEnabled,
		Labels:           nil,
		Annotations:      nil,
		Health:           c.Health,
		Created:          c.Created,
		CreatedAt:        c.CreatedAt,
		LastSync:         c.LastSync,
	}
	// Nodes
	if c.Nodes != nil {
		clone.Nodes = make(map[string]ClusterNodeState, len(c.Nodes))
		for k, v := range c.Nodes {
			clone.Nodes[k] = v.Clone()
		}
	} else {
		clone.Nodes = make(map[string]ClusterNodeState)
	}
	// HelmReleases
	if c.HelmReleases != nil {
		clone.HelmReleases = make(map[string]HelmReleaseInfo, len(c.HelmReleases))
		for k, v := range c.HelmReleases {
			vv := v.Clone()
			clone.HelmReleases[k] = *vv
		}
	} else {
		clone.HelmReleases = make(map[string]HelmReleaseInfo)
	}
	// StorageClasses slice
	if c.StorageClasses != nil {
		clone.StorageClasses = make([]string, len(c.StorageClasses))
		copy(clone.StorageClasses, c.StorageClasses)
	}
	// Labels & Annotations
	if c.Labels != nil {
		clone.Labels = make(map[string]string, len(c.Labels))
		for k, v := range c.Labels {
			clone.Labels[k] = v
		}
	}
	if c.Annotations != nil {
		clone.Annotations = make(map[string]string, len(c.Annotations))
		for k, v := range c.Annotations {
			clone.Annotations[k] = v
		}
	}
	return clone
}

// Clone creates a deep copy of State
func (s *State) Clone() *State {
	if s == nil {
		return nil
	}
	c := &State{
		Version:  s.Version,
		Commit:   s.Commit,
		File:     s.File,
		LastSync: s.LastSync,
	}

	// Paths
	c.Paths = s.Paths

	// Machine
	m := s.Machine.Clone()
	c.Machine = *m

	// Cluster
	cc := s.Cluster.Clone()
	c.Cluster = *cc

	// BlockNode
	bc := s.BlockNode.Clone()
	c.BlockNode = *bc

	return c
}
