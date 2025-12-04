package core

import "time"

// helper to create *time.Time
func timePtr(t time.Time) *time.Time {
	return &t
}

// helper to clone *time.Time
func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}

// Clone creates a deep copy of SoftwareState
func (s *SoftwareState) Clone() SoftwareState {
	return SoftwareState{
		Name:        s.Name,
		Version:     s.Version,
		Source:      s.Source,
		Installed:   s.Installed,
		InstalledAt: cloneTime(s.InstalledAt),
		LastSync:    cloneTime(s.LastSync),
	}
}

// Clone creates a deep copy of StorageState
func (s *StorageState) Clone() StorageState {
	return StorageState{
		Name:      s.Name,
		Mounted:   s.Mounted,
		MountedAt: cloneTime(s.MountedAt),
	}
}

// Clone creates a deep copy of BlockNodeState
func (b *BlockNodeState) Clone() *BlockNodeState {
	if b == nil {
		return nil
	}
	clone := &BlockNodeState{
		Created:   b.Created,
		CreatedAt: cloneTime(b.CreatedAt),
		LastSync:  cloneTime(b.LastSync),
	}
	if b.Config != nil {
		cfg := *b.Config
		clone.Config = &cfg
	}
	return clone
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
		LastSync:    cloneTime(n.LastSync),
	}
}

// Clone creates a deep copy of HelmReleaseState
func (h *HelmReleaseState) Clone() HelmReleaseState {
	return HelmReleaseState{
		Name:      h.Name,
		Namespace: h.Namespace,
		Chart:     h.Chart,
		Version:   h.Version,
		Deployed:  h.Deployed,
		UpdatedAt: cloneTime(h.UpdatedAt),
		Notes:     h.Notes,
		LastSync:  cloneTime(h.LastSync),
	}
}

// Clone creates a deep copy of MachineState
func (m *MachineState) Clone() *MachineState {
	if m == nil {
		return nil
	}
	clone := &MachineState{
		Initialized:   m.Initialized,
		InitializedAt: cloneTime(m.InitializedAt),
		LastSync:      cloneTime(m.LastSync),
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
		CreatedAt:        cloneTime(c.CreatedAt),
		LastSync:         cloneTime(c.LastSync),
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
		clone.HelmReleases = make(map[string]HelmReleaseState, len(c.HelmReleases))
		for k, v := range c.HelmReleases {
			clone.HelmReleases[k] = v.Clone()
		}
	} else {
		clone.HelmReleases = make(map[string]HelmReleaseState)
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
		LastSync: cloneTime(s.LastSync),
	}
	// Paths (shallow copy of struct value)
	if s.Paths != nil {
		p := *s.Paths
		c.Paths = &p
	}
	// Machine
	c.Machine = s.Machine.Clone()
	// Cluster
	c.Cluster = s.Cluster.Clone()
	// BlockNode
	c.BlockNode = s.BlockNode.Clone()
	return c
}
