// SPDX-License-Identifier: Apache-2.0

package hardware

// consensusNode represents a consensus node with its specific requirements and validation logic
type consensusNode struct {
	baseNode
}

// Ensure consensusNode implements Spec
var _ Spec = (*consensusNode)(nil)

// NewConsensusNodeSpec creates a new consensus node specification checker with SystemInfo interface
func NewConsensusNodeSpec(hostProfile HostProfile) Spec {
	return &consensusNode{
		baseNode: baseNode{
			nodeType:          "Consensus Node",
			actualHostProfile: hostProfile,
			minimalRequirements: BaselineRequirements{
				MinCpuCores:    16,
				MinMemoryGB:    32,
				MinStorageGB:   1000,
				MinSupportedOS: []string{"Ubuntu 20", "Debian 11"},
			},
		},
	}
}
