// SPDX-License-Identifier: Apache-2.0

package state

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

// Clone creates a deep copy of BlockNodeState
func (b *BlockNodeState) Clone() BlockNodeState {
	clone := *b
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
		LastSync:    n.LastSync,
	}
}

// Clone creates a deep copy of HelmReleaseInfo
func (h *HelmReleaseInfo) Clone() HelmReleaseInfo {
	c := *h
	return c
}

// Clone creates a deep copy of MachineState
func (m *MachineState) Clone() MachineState {
	clone := MachineState{
		LastSync: m.LastSync,
	}
	if m.Software != nil {
		clone.Software = make(map[string]SoftwareState, len(m.Software))
		for k, v := range m.Software {
			clone.Software[k] = v.Clone()
		}
	} else {
		clone.Software = make(map[string]SoftwareState)
	}

	return clone
}

// Clone creates a deep copy of ClusterState
func (cs *ClusterState) Clone() ClusterState {
	clone := *cs
	return clone
}

// Clone creates a deep copy of State
func (s *State) Clone() State {
	c := State{
		ProvisionerState: s.ProvisionerState,
		StateFile:        s.StateFile,
		LastSync:         s.LastSync,
	}

	// MachineState
	c.MachineState = s.MachineState.Clone()

	// ClusterState
	c.ClusterState = s.ClusterState.Clone()

	// BlockNodeState
	c.BlockNodeState = s.BlockNodeState.Clone()

	return c
}
