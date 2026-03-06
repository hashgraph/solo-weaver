// SPDX-License-Identifier: Apache-2.0

package state

// Equal returns true if two SoftwareState values are equal, ignoring LastSync.
func (s *SoftwareState) Equal(other SoftwareState) bool {
	if s.Name != other.Name ||
		s.Version != other.Version ||
		s.Installed != other.Installed ||
		s.Configured != other.Configured {
		return false
	}
	if len(s.Metadata) != len(other.Metadata) {
		return false
	}
	for k, v := range s.Metadata {
		if other.Metadata[k] != v {
			return false
		}
	}
	return true
}

// Equal returns true if two HardwareState values are equal, ignoring LastSync.
func (h *HardwareState) Equal(other HardwareState) bool {
	if h.Type != other.Type ||
		h.Info != other.Info ||
		h.Count != other.Count ||
		h.Size != other.Size {
		return false
	}
	if len(h.Metadata) != len(other.Metadata) {
		return false
	}
	for k, v := range h.Metadata {
		if other.Metadata[k] != v {
			return false
		}
	}
	return true
}

// Equal returns true if two MachineState values are equal, ignoring LastSync.
func (m *MachineState) Equal(other MachineState) bool {
	if len(m.Software) != len(other.Software) || len(m.Hardware) != len(other.Hardware) {
		return false
	}
	for name, sw := range m.Software {
		otherSw, ok := other.Software[name]
		if !ok || !sw.Equal(otherSw) {
			return false
		}
	}
	for name, hw := range m.Hardware {
		otherHw, ok := other.Hardware[name]
		if !ok || !hw.Equal(otherHw) {
			return false
		}
	}
	return true
}

// Equal returns true if two ClusterNodeState values are equal, ignoring LastSync.
func (c *ClusterNodeState) Equal(other ClusterNodeState) bool {
	if c.Name != other.Name ||
		c.Role != other.Role ||
		c.Ready != other.Ready ||
		c.KubeletVer != other.KubeletVer {
		return false
	}
	if len(c.Labels) != len(other.Labels) {
		return false
	}
	for k, v := range c.Labels {
		if other.Labels[k] != v {
			return false
		}
	}
	if len(c.Annotations) != len(other.Annotations) {
		return false
	}
	for k, v := range c.Annotations {
		if other.Annotations[k] != v {
			return false
		}
	}
	return true
}

// Equal returns true if two HelmReleaseInfo values are equal, ignoring time fields.
func (h *HelmReleaseInfo) Equal(other HelmReleaseInfo) bool {
	return h.Name == other.Name &&
		h.Version == other.Version &&
		h.Namespace == other.Namespace &&
		h.ChartRef == other.ChartRef &&
		h.ChartName == other.ChartName &&
		h.ChartVersion == other.ChartVersion &&
		h.Status == other.Status
}

// Equal returns true if two ClusterState values are equal, ignoring LastSync.
func (c *ClusterState) Equal(other ClusterState) bool {
	return c.Created == other.Created && c.ClusterInfo.Equal(other.ClusterInfo)
}

// Equal returns true if two BlockNodeState values are equal, ignoring LastSync.
func (b *BlockNodeState) Equal(other BlockNodeState) bool {
	return b.ReleaseInfo.Equal(other.ReleaseInfo) &&
		b.Storage == other.Storage
}
