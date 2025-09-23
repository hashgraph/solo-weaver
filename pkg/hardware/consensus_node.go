package hardware

import (
	"fmt"
)

// consensusNode represents a consensus node with its specific requirements and validation logic
type consensusNode struct {
	nodeType            string
	actualHostProfile   HostProfile
	minimalRequirements BaselineRequirements
}

// Ensure consensusNode implements Spec
var _ Spec = (*consensusNode)(nil)

// NewConsensusNodeSpec creates a new consensus node specification checker with SystemInfo interface
func NewConsensusNodeSpec(hostProfile HostProfile) Spec {
	return &consensusNode{
		nodeType:          "Consensus Node",
		actualHostProfile: hostProfile,
		minimalRequirements: BaselineRequirements{
			MinCpuCores:    16,
			MinMemoryGB:    32,
			MinStorageGB:   1000,
			MinSupportedOS: []string{"Ubuntu 20", "Debian 11"},
		},
	}
}

// ValidateOS validates OS requirements for consensus node
func (c *consensusNode) ValidateOS() error {
	if !validateOS(c.minimalRequirements.MinSupportedOS, c.actualHostProfile) {
		return fmt.Errorf("OS does not meet %s requirements (supported: %v)", c.nodeType, c.minimalRequirements.MinSupportedOS)
	}
	return nil
}

// ValidateCPU validates CPU requirements for consensus node
func (c *consensusNode) ValidateCPU() error {
	cores := c.actualHostProfile.GetCPUCores()
	if int(cores) < c.minimalRequirements.MinCpuCores {
		return fmt.Errorf("CPU does not meet %s requirements (minimum %d cores)", c.nodeType, c.minimalRequirements.MinCpuCores)
	}
	return nil
}

// ValidateMemory validates memory requirements for consensus node
func (c *consensusNode) ValidateMemory() error {
	totalMemoryGB := c.actualHostProfile.GetTotalMemoryGB()
	if int(totalMemoryGB) < c.minimalRequirements.MinMemoryGB {
		return fmt.Errorf("memory does not meet %s requirements (minimum %d GB)", c.nodeType, c.minimalRequirements.MinMemoryGB)
	}
	return nil
}

// ValidateStorage validates storage requirements for consensus node
func (c *consensusNode) ValidateStorage() error {
	totalStorageGB := c.actualHostProfile.GetTotalStorageGB()
	if int(totalStorageGB) < c.minimalRequirements.MinStorageGB {
		return fmt.Errorf("storage does not meet %s requirements (minimum %d GB)", c.nodeType, c.minimalRequirements.MinStorageGB)
	}
	return nil
}

// GetBaselineRequirements returns the requirements for consensus node
func (c *consensusNode) GetBaselineRequirements() BaselineRequirements {
	return c.minimalRequirements
}

// GetNodeType returns the node type
func (c *consensusNode) GetNodeType() string {
	return c.nodeType
}
