package hardware

import (
	"fmt"
)

// blockNode represents a block node with its specific requirements and validation logic
type blockNode struct {
	nodeType            string
	actualHostProfile   HostProfile
	minimalRequirements BaselineRequirements
}

// Ensure blockNode implements Spec
var _ Spec = (*blockNode)(nil)

// NewBlockNodeSpec creates a new block node specification checker with SystemInfo interface
func NewBlockNodeSpec(hostProfile HostProfile) Spec {
	return &blockNode{
		nodeType:          "Block Node",
		actualHostProfile: hostProfile,
		minimalRequirements: BaselineRequirements{
			MinCpuCores:    8,
			MinMemoryGB:    16,
			MinStorageGB:   5000,
			MinSupportedOS: []string{"Ubuntu 18", "Debian 10"},
		},
	}
}

// ValidateOS validates OS requirements for block node
func (b *blockNode) ValidateOS() error {
	if !validateOS(b.minimalRequirements.MinSupportedOS, b.actualHostProfile) {
		return fmt.Errorf("OS does not meet %s requirements (supported: %v)", b.nodeType, b.minimalRequirements.MinSupportedOS)
	}
	return nil
}

// ValidateCPU validates CPU requirements for block node
func (b *blockNode) ValidateCPU() error {
	cores := b.actualHostProfile.GetCPUCores()
	if int(cores) < b.minimalRequirements.MinCpuCores {
		return fmt.Errorf("CPU does not meet %s requirements (minimum %d cores)", b.nodeType, b.minimalRequirements.MinCpuCores)
	}
	return nil
}

// ValidateMemory validates memory requirements for block node
// Checks both total and available memory to ensure the system has sufficient resources
func (b *blockNode) ValidateMemory() error {
	totalMemoryGB := b.actualHostProfile.GetTotalMemoryGB()
	availableMemoryGB := b.actualHostProfile.GetAvailableMemoryGB()

	// Check total memory first
	if int(totalMemoryGB) < b.minimalRequirements.MinMemoryGB {
		return fmt.Errorf("total memory does not meet %s requirements (minimum %d GB, found %d GB total)",
			b.nodeType, b.minimalRequirements.MinMemoryGB, totalMemoryGB)
	}

	// Check available memory (should be at least 80% of required minimum)
	minAvailableMemoryGB := int(float64(b.minimalRequirements.MinMemoryGB) * 0.8)
	if int(availableMemoryGB) < minAvailableMemoryGB {
		return fmt.Errorf("available memory does not meet %s requirements (minimum %d GB available, found %d GB available out of %d GB total)",
			b.nodeType, minAvailableMemoryGB, availableMemoryGB, totalMemoryGB)
	}

	return nil
}

// ValidateStorage validates storage requirements for block node
func (b *blockNode) ValidateStorage() error {
	totalStorageGB := b.actualHostProfile.GetTotalStorageGB()
	if int(totalStorageGB) < b.minimalRequirements.MinStorageGB {
		return fmt.Errorf("storage does not meet %s requirements (minimum %d GB)", b.nodeType, b.minimalRequirements.MinStorageGB)
	}
	return nil
} // GetBaselineRequirements returns the requirements for block node
func (b *blockNode) GetBaselineRequirements() BaselineRequirements {
	return b.minimalRequirements
}

// GetNodeType returns the node type
func (b *blockNode) GetNodeType() string {
	return b.nodeType
}
