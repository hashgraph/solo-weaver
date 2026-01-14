// SPDX-License-Identifier: Apache-2.0

package hardware

// localNode represents a local node with its specific requirements and validation logic
type localNode struct {
	baseNode
}

// Ensure localNode implements Spec
var _ Spec = (*localNode)(nil)

// NewLocalNodeSpec creates a new local node specification checker with SystemInfo interface
func NewLocalNodeSpec(hostProfile HostProfile) Spec {
	return &localNode{
		baseNode: baseNode{
			nodeType:          "Local Node",
			actualHostProfile: hostProfile,
			minimalRequirements: BaselineRequirements{
				MinCpuCores:    3,
				MinMemoryGB:    1,
				MinStorageGB:   1,
				MinSupportedOS: []string{"Ubuntu 18", "Debian 10"},
			},
		},
	}
}
