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
	MinCpuCores    int
	MinMemoryGB    int
	MinStorageGB   int
	MinSupportedOS []string
}

func (r BaselineRequirements) String() string {
	return fmt.Sprintf("OS: %v, CPU: %d cores, Memory: %d GB, Storage: %d GB, ",
		r.MinSupportedOS, r.MinCpuCores, r.MinMemoryGB, r.MinStorageGB)
}
