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
			// Note: The minimum CPU cores requirement is set to 3 (instead of 1) to ensure
			// that observability components (e.g., Alloy, Node Exporter, etc.) can run
			// alongside the local cluster workloads on the same node.
			minimalRequirements: BaselineRequirements{
				MinCpuCores:    3,
				MinMemoryGB:    1,
				MinStorageGB:   1,
				MinSupportedOS: []string{"Ubuntu 18", "Debian 10"},
			},
		},
	}
}
