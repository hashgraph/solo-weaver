package hardware

import (
	"fmt"
)

// localNode represents a local node with its specific requirements and validation logic
type localNode struct {
	nodeType            string
	actualHostProfile   HostProfile
	minimalRequirements BaselineRequirements
}

// Ensure localNode implements Spec
var _ Spec = (*localNode)(nil)

// NewLocalNodeSpec creates a new local node specification checker with SystemInfo interface
func NewLocalNodeSpec(hostProfile HostProfile) Spec {
	return &localNode{
		nodeType:          "Local Node",
		actualHostProfile: hostProfile,
		minimalRequirements: BaselineRequirements{
			MinCpuCores:    1,
			MinMemoryGB:    1,
			MinStorageGB:   500,
			MinSupportedOS: []string{"Ubuntu 18", "Debian 10"},
		},
	}
}

// ValidateOS validates OS requirements for local node
func (l *localNode) ValidateOS() error {
	if !validateOS(l.minimalRequirements.MinSupportedOS, l.actualHostProfile) {
		return fmt.Errorf("OS does not meet %s requirements (supported: %v)", l.nodeType, l.minimalRequirements.MinSupportedOS)
	}
	return nil
}

// ValidateCPU validates CPU requirements for local node
func (l *localNode) ValidateCPU() error {
	cores := l.actualHostProfile.GetCPUCores()
	if int(cores) < l.minimalRequirements.MinCpuCores {
		return fmt.Errorf("CPU does not meet %s requirements (minimum %d cores)", l.nodeType, l.minimalRequirements.MinCpuCores)
	}
	return nil
}

// ValidateMemory validates memory requirements for local node
// Checks both total and available memory to ensure the system has sufficient resources
func (l *localNode) ValidateMemory() error {
	totalMemoryGB := l.actualHostProfile.GetTotalMemoryGB()
	availableMemoryGB := l.actualHostProfile.GetAvailableMemoryGB()

	// Check total memory first
	if int(totalMemoryGB) < l.minimalRequirements.MinMemoryGB {
		return fmt.Errorf("total memory does not meet %s requirements (minimum %d GB, found %d GB total)",
			l.nodeType, l.minimalRequirements.MinMemoryGB, totalMemoryGB)
	}

	// Check if application is already running
	isApplicationRunning := l.actualHostProfile.IsNodeAlreadyRunning()

	systemBufferGB := 0.5 // 512MB buffer for system operations

	if isApplicationRunning {
		// If app is running, we just need enough for system operations
		if float64(availableMemoryGB) < systemBufferGB {
			return fmt.Errorf("insufficient available memory for system operations while %s is running (need %.1f GB, have %.1f GB available)",
				l.nodeType, systemBufferGB, float64(availableMemoryGB))
		}
	} else {
		// Fresh installation needs full requirement plus buffer
		requiredAvailableGB := float64(l.minimalRequirements.MinMemoryGB) + systemBufferGB
		if float64(availableMemoryGB) < requiredAvailableGB {
			// Calculate how much memory might be used by existing processes
			usedMemoryGB := totalMemoryGB - availableMemoryGB

			return fmt.Errorf("insufficient available memory for fresh %s installation (need %.1f GB including system buffer, have %.1f GB available, %.1f GB currently used)",
				l.nodeType, requiredAvailableGB, float64(availableMemoryGB), float64(usedMemoryGB))
		}
	}

	return nil
}

// ValidateStorage validates storage requirements for local node
func (l *localNode) ValidateStorage() error {
	totalStorageGB := l.actualHostProfile.GetTotalStorageGB()
	if int(totalStorageGB) < l.minimalRequirements.MinStorageGB {
		return fmt.Errorf("storage does not meet %s requirements (minimum %d GB)", l.nodeType, l.minimalRequirements.MinStorageGB)
	}
	return nil
} // GetBaselineRequirements returns the requirements for local node
func (l *localNode) GetBaselineRequirements() BaselineRequirements {
	return l.minimalRequirements
}

// GetNodeType returns the node type
func (l *localNode) GetNodeType() string {
	return l.nodeType
}
