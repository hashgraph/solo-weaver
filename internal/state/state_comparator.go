// SPDX-License-Identifier: Apache-2.0

package state

import "github.com/hashgraph/solo-weaver/pkg/models"

// Equal returns true if two SoftwareState values are equal, ignoring LastSync.
func (s *SoftwareState) Equal(other SoftwareState) bool {
	if s.Name != other.Name ||
		s.Version != other.Version ||
		s.Installed != other.Installed ||
		s.Configured != other.Configured {
		return false
	}
	return models.IsEqualMap(s.Metadata, other.Metadata)
}

// Equal returns true if two HardwareState values are equal, ignoring LastSync.
func (s *HardwareState) Equal(other HardwareState) bool {
	if s.Type != other.Type ||
		s.Info != other.Info ||
		s.Count != other.Count ||
		s.Size != other.Size {
		return false
	}
	return models.IsEqualMap(s.Metadata, other.Metadata)
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

// Equal returns true if two HelmReleaseInfo values are equal, ignoring time fields
func (h *HelmReleaseInfo) Equal(other HelmReleaseInfo) bool {
	return h.Name == other.Name &&
		h.ChartVersion == other.ChartVersion &&
		h.Namespace == other.Namespace &&
		h.ChartRef == other.ChartRef &&
		h.ChartName == other.ChartName &&
		h.Status == other.Status &&
		h.AppVersion == other.AppVersion
}

// Equal returns true if two ClusterState values are equal, ignoring LastSync.
func (cs *ClusterState) Equal(other ClusterState) bool {
	return cs.Created == other.Created && cs.ClusterInfo.Equal(other.ClusterInfo)
}

// Equal returns true if two BlockNodeState values are equal, ignoring LastSync.
func (b *BlockNodeState) Equal(other BlockNodeState) bool {
	return b.ReleaseInfo.Equal(other.ReleaseInfo) &&
		b.Storage == other.Storage
}

// Equal returns true if two ConsensusNodeManagedSpec values describe the same
// managed shape. It compares the stable hash (which covers every field, including
// the Volumes map and the pull-secret selector), so a nil and an empty-but-non-nil
// spec are treated as different (nil = "never recorded", {} = "recorded, all
// defaults"). Callers handle the nil/nil case before dereferencing.
func (m *ConsensusNodeManagedSpec) Equal(other *ConsensusNodeManagedSpec) bool {
	if m == nil || other == nil {
		return m == nil && other == nil
	}
	return m.Hash() == other.Hash()
}

// Equal returns true if two ConsensusNodeState values describe the same managed
// node, ignoring LastSync and the per-run deployment-package dir (a host path
// that changes per invocation — see issue #1187 note 4). It compares identity,
// the config-content hashes (by hash + source, ignoring LastUpdate), and the
// managed shape.
func (n *ConsensusNodeState) Equal(other ConsensusNodeState) bool {
	if n.Namespace != other.Namespace ||
		n.OrbitName != other.OrbitName ||
		n.NodeId != other.NodeId ||
		n.AccountId != other.AccountId ||
		n.Weight != other.Weight ||
		n.ImageRepo != other.ImageRepo ||
		n.ImageTag != other.ImageTag ||
		n.LedgerId != other.LedgerId ||
		n.ChainId != other.ChainId ||
		n.GrpcTlsSecret != other.GrpcTlsSecret ||
		n.SigningSecret != other.SigningSecret {
		return false
	}
	if !equalConfigHashes(n.ConfigHashes, other.ConfigHashes) {
		return false
	}
	return n.ManagedSpec.Equal(other.ManagedSpec)
}

// equalConfigHashes compares two config-hash maps by content hash and source,
// ignoring each entry's LastUpdate timestamp.
func equalConfigHashes(a, b map[string]ConfigHashEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || av.Hash != bv.Hash || av.Source != bv.Source {
			return false
		}
	}
	return true
}
