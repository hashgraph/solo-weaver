// SPDX-License-Identifier: Apache-2.0

package hardware

import (
	"fmt"
)

const (
	// SystemBufferGB defines the memory buffer reserved for system operations (in GB)
	SystemBufferGB = 0.5 // 512MB buffer for system operations
)

// baseNode provides common validation logic for all node types
type baseNode struct {
	nodeType            string
	actualHostProfile   HostProfile
	minimalRequirements BaselineRequirements
}

// ValidateOS validates OS requirements using common logic
func (b *baseNode) ValidateOS() error {
	if !validateOS(b.minimalRequirements.MinSupportedOS, b.actualHostProfile) {
		return fmt.Errorf("OS does not meet %s requirements (supported: %v)", b.nodeType, b.minimalRequirements.MinSupportedOS)
	}
	return nil
}

// ValidateCPU validates CPU requirements using common logic
func (b *baseNode) ValidateCPU() error {
	cores := b.actualHostProfile.GetCPUCores()
	if int(cores) < b.minimalRequirements.MinCpuCores {
		return fmt.Errorf("CPU does not meet %s requirements (minimum %d cores)", b.nodeType, b.minimalRequirements.MinCpuCores)
	}
	return nil
}

// ValidateMemory validates memory requirements using the local node logic
// Checks both total and available memory to ensure the system has sufficient resources
func (b *baseNode) ValidateMemory() error {
	totalMemoryGB := b.actualHostProfile.GetTotalMemoryGB()
	availableMemoryGB := b.actualHostProfile.GetAvailableMemoryGB()

	// Check total memory first
	if int(totalMemoryGB) < b.minimalRequirements.MinMemoryGB {
		return fmt.Errorf("total memory does not meet %s requirements (minimum %d GB, found %d GB total)",
			b.nodeType, b.minimalRequirements.MinMemoryGB, totalMemoryGB)
	}

	// Check if application is already running
	isApplicationRunning := b.actualHostProfile.IsNodeAlreadyRunning()

	if isApplicationRunning {
		// If app is running, we just need enough for system operations
		if float64(availableMemoryGB) < SystemBufferGB {
			return fmt.Errorf("insufficient available memory for system operations while %s is running (need %.1f GB, have %.1f GB available)",
				b.nodeType, SystemBufferGB, float64(availableMemoryGB))
		}
	} else {
		// Fresh installation needs full requirement plus buffer
		requiredAvailableGB := float64(b.minimalRequirements.MinMemoryGB) + SystemBufferGB
		if float64(availableMemoryGB) < requiredAvailableGB {
			// Calculate how much memory might be used by existing processes
			usedMemoryGB := totalMemoryGB - availableMemoryGB

			return fmt.Errorf("insufficient available memory for fresh %s installation (need %.1f GB including system buffer, have %.1f GB available, %.1f GB currently used)",
				b.nodeType, requiredAvailableGB, float64(availableMemoryGB), float64(usedMemoryGB))
		}
	}

	return nil
}

// ValidateStorage validates storage requirements using common logic.
// If MinSSDStorageGB or MinHDDStorageGB are set, validates those separately.
// Otherwise, validates total storage against MinStorageGB.
func (b *baseNode) ValidateStorage() error {
	// Check if we need to validate SSD/HDD separately
	if b.minimalRequirements.MinSSDStorageGB > 0 || b.minimalRequirements.MinHDDStorageGB > 0 {
		return b.validateSplitStorage()
	}

	// Default: validate total storage
	totalStorageGB := b.actualHostProfile.GetTotalStorageGB()
	if int(totalStorageGB) < b.minimalRequirements.MinStorageGB {
		return fmt.Errorf("storage does not meet %s requirements (minimum %d GB, found %d GB)",
			b.nodeType, b.minimalRequirements.MinStorageGB, totalStorageGB)
	}
	return nil
}

// validateSplitStorage validates SSD and HDD storage separately
func (b *baseNode) validateSplitStorage() error {
	var errors []string

	if b.minimalRequirements.MinSSDStorageGB > 0 {
		ssdStorageGB := b.actualHostProfile.GetSSDStorageGB()
		if int(ssdStorageGB) < b.minimalRequirements.MinSSDStorageGB {
			errors = append(errors, fmt.Sprintf("SSD/NVMe storage: minimum %d GB required, found %d GB",
				b.minimalRequirements.MinSSDStorageGB, ssdStorageGB))
		}
	}

	if b.minimalRequirements.MinHDDStorageGB > 0 {
		hddStorageGB := b.actualHostProfile.GetHDDStorageGB()
		if int(hddStorageGB) < b.minimalRequirements.MinHDDStorageGB {
			errors = append(errors, fmt.Sprintf("HDD storage: minimum %d GB required, found %d GB",
				b.minimalRequirements.MinHDDStorageGB, hddStorageGB))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("storage does not meet %s requirements: %s", b.nodeType, joinErrors(errors))
	}
	return nil
}

// joinErrors joins multiple error messages with "; "
func joinErrors(errors []string) string {
	result := ""
	for i, e := range errors {
		if i > 0 {
			result += "; "
		}
		result += e
	}
	return result
}

// GetBaselineRequirements returns the requirements
func (b *baseNode) GetBaselineRequirements() BaselineRequirements {
	return b.minimalRequirements
}

// GetNodeType returns the node type
func (b *baseNode) GetNodeType() string {
	return b.nodeType
}
