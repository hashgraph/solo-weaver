// SPDX-License-Identifier: Apache-2.0

package state

import "github.com/hashgraph/solo-weaver/pkg/models"

// Clone creates a deep copy of SoftwareState
func (s *SoftwareState) Clone() (*SoftwareState, error) {
	var meta models.StringMap
	var err error
	if s.Metadata != nil {
		meta, err = s.Metadata.Clone()
		if err != nil {
			return nil, err
		}
	}
	return &SoftwareState{
		Name:       s.Name,
		Version:    s.Version,
		Installed:  s.Installed,
		Configured: s.Configured,
		Metadata:   meta,
		LastSync:   s.LastSync,
	}, nil
}

func (s *HardwareState) Clone() (*HardwareState, error) {
	var meta models.StringMap
	var err error
	if s.Metadata != nil {
		meta, err = s.Metadata.Clone()
		if err != nil {
			return nil, err
		}
	}

	return &HardwareState{
		Type:     s.Type,
		Info:     s.Info,
		Count:    s.Count,
		Size:     s.Size,
		Metadata: meta,
		LastSync: s.LastSync,
	}, nil
}

// Clone creates a deep copy of BlockNodeState
func (b *BlockNodeState) Clone() (*BlockNodeState, error) {
	clone := *b
	return &clone, nil
}

// Clone creates a deep copy of ClusterNodeState
func (c *ClusterNodeState) Clone() (*ClusterNodeState, error) {
	var labels map[string]string
	if c.Labels != nil {
		labels = make(map[string]string, len(c.Labels))
		for k, v := range c.Labels {
			labels[k] = v
		}
	}
	var ann map[string]string
	if c.Annotations != nil {
		ann = make(map[string]string, len(c.Annotations))
		for k, v := range c.Annotations {
			ann[k] = v
		}
	}
	return &ClusterNodeState{
		Name:        c.Name,
		Role:        c.Role,
		Ready:       c.Ready,
		KubeletVer:  c.KubeletVer,
		Labels:      labels,
		Annotations: ann,
		LastSync:    c.LastSync,
	}, nil
}

// Clone creates a deep copy of HelmReleaseInfo
func (h *HelmReleaseInfo) Clone() (*HelmReleaseInfo, error) {
	c := *h
	return &c, nil
}

// Clone creates a deep copy of MachineState
func (m *MachineState) Clone() (*MachineState, error) {
	clone := MachineState{
		LastSync: m.LastSync,
	}
	if m.Software != nil {
		clone.Software = make(map[string]SoftwareState, len(m.Software))
		for k, v := range m.Software {
			sc, err := v.Clone()
			if err != nil {
				return nil, err
			}

			clone.Software[k] = *sc
		}
	} else {
		clone.Software = make(map[string]SoftwareState)
	}

	if m.Hardware != nil {
		clone.Hardware = make(map[string]HardwareState, len(m.Hardware))
		for k, v := range m.Hardware {
			hc, err := v.Clone()
			if err != nil {
				return nil, err
			}

			clone.Hardware[k] = *hc
		}
	} else {
		clone.Hardware = make(map[string]HardwareState)
	}

	return &clone, nil
}

// Clone creates a deep copy of ClusterState
func (cs *ClusterState) Clone() (*ClusterState, error) {
	clone := *cs
	return &clone, nil
}

// Clone creates a deep copy of State
func (s *State) Clone() (*State, error) {
	c := State{
		ProvisionerState: s.ProvisionerState,
		StateFile:        s.StateFile,
		LastSync:         s.LastSync,
	}

	// MachineState
	ms, err := s.MachineState.Clone()
	if err != nil {
		return nil, err
	}
	c.MachineState = *ms

	// ClusterState
	cs, err := s.ClusterState.Clone()
	if err != nil {
		return nil, err
	}
	c.ClusterState = *cs

	// BlockNodeState
	bs, err := s.BlockNodeState.Clone()
	if err != nil {
		return nil, err
	}
	c.BlockNodeState = *bs

	return &c, err
}
