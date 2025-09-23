package hardware

// blockNode represents a block node with its specific requirements and validation logic
type blockNode struct {
	baseNode
}

// Ensure blockNode implements Spec
var _ Spec = (*blockNode)(nil)

// NewBlockNodeSpec creates a new block node specification checker with SystemInfo interface
func NewBlockNodeSpec(hostProfile HostProfile) Spec {
	return &blockNode{
		baseNode: baseNode{
			nodeType:          "Block Node",
			actualHostProfile: hostProfile,
			minimalRequirements: BaselineRequirements{
				MinCpuCores:    8,
				MinMemoryGB:    16,
				MinStorageGB:   5000,
				MinSupportedOS: []string{"Ubuntu 18", "Debian 10"},
			},
		},
	}
}
