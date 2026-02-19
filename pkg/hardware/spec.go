// SPDX-License-Identifier: Apache-2.0

package hardware

import (
	"fmt"
)

type Spec interface {
	ValidateOS() error
	ValidateCPU() error
	ValidateMemory() error
	ValidateStorage() error

	GetBaselineRequirements() BaselineRequirements
	GetNodeType() string
}

type BaselineRequirements struct {
	MinCpuCores     int
	MinMemoryGB     int
	MinStorageGB    int // Total storage (used when SSD/HDD split not required)
	MinSSDStorageGB int // Minimum SSD/NVMe storage (0 means not required)
	MinHDDStorageGB int // Minimum HDD storage (0 means not required)
	MinSupportedOS  []string
}

func (r BaselineRequirements) String() string {
	if r.MinSSDStorageGB > 0 || r.MinHDDStorageGB > 0 {
		return fmt.Sprintf("OS: %v, CPU: %d cores, Memory: %d GB, SSD: %d GB, HDD: %d GB",
			r.MinSupportedOS, r.MinCpuCores, r.MinMemoryGB, r.MinSSDStorageGB, r.MinHDDStorageGB)
	}
	return fmt.Sprintf("OS: %v, CPU: %d cores, Memory: %d GB, Storage: %d GB",
		r.MinSupportedOS, r.MinCpuCores, r.MinMemoryGB, r.MinStorageGB)
}
