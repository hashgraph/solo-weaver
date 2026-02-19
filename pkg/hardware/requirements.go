// SPDX-License-Identifier: Apache-2.0

package hardware

import "github.com/hashgraph/solo-weaver/internal/core"

// Supported OS constants
const (
	OSUbuntu18 = "Ubuntu 18"
	OSDebian10 = "Debian 10"
)

// Common OS requirement sets
var (
	supportedOS = []string{OSUbuntu18, OSDebian10}
)

// requirementsRegistry holds the hardware requirements for each (nodeType, profile) combination.
// This design separates the two orthogonal concerns:
// - Node Type: what kind of node (block, consensus)
// - Profile/Environment: where it runs (local, testnet, mainnet, previewnet, perfnet)
//
// The requirements are looked up as: registry[nodeType][profile] -> BaselineRequirements
var requirementsRegistry = map[string]map[string]BaselineRequirements{
	// Block Node requirements per environment
	core.NodeTypeBlock: {
		core.ProfileLocal: {
			MinCpuCores:    3,
			MinMemoryGB:    1,
			MinStorageGB:   1,
			MinSupportedOS: supportedOS,
		},
		core.ProfilePerfnet: {
			MinCpuCores:    8,
			MinMemoryGB:    16,
			MinStorageGB:   5000,
			MinSupportedOS: supportedOS,
		},
		core.ProfileTestnet: {
			MinCpuCores:     48,
			MinMemoryGB:     256,
			MinSSDStorageGB: 8000,  // 8TB NVMe/SSD
			MinHDDStorageGB: 24000, // 24TB HDD
			MinSupportedOS:  supportedOS,
		},
		core.ProfilePreviewnet: {
			MinCpuCores:     48,
			MinMemoryGB:     256,
			MinSSDStorageGB: 8000,  // 8TB NVMe/SSD
			MinHDDStorageGB: 24000, // 24TB HDD
			MinSupportedOS:  supportedOS,
		},
		core.ProfileMainnet: {
			MinCpuCores:     48,
			MinMemoryGB:     256,
			MinSSDStorageGB: 8000,  // 8TB NVMe/SSD
			MinHDDStorageGB: 24000, // 24TB HDD
			MinSupportedOS:  supportedOS,
		},
	},

	// Consensus Node requirements per environment
	core.NodeTypeConsensus: {
		core.ProfileLocal: {
			MinCpuCores:    3,
			MinMemoryGB:    1,
			MinStorageGB:   1,
			MinSupportedOS: supportedOS,
		},
		core.ProfilePerfnet: {
			MinCpuCores:    16,
			MinMemoryGB:    32,
			MinStorageGB:   1000,
			MinSupportedOS: supportedOS,
		},
		core.ProfileTestnet: {
			MinCpuCores:    16,
			MinMemoryGB:    32,
			MinStorageGB:   1000,
			MinSupportedOS: supportedOS,
		},
		core.ProfilePreviewnet: {
			MinCpuCores:    48,
			MinMemoryGB:    256,
			MinStorageGB:   8000,
			MinSupportedOS: supportedOS,
		},
		core.ProfileMainnet: {
			MinCpuCores:    48,
			MinMemoryGB:    256,
			MinStorageGB:   8000,
			MinSupportedOS: supportedOS,
		},
	},
}

// GetRequirements returns the hardware requirements for a given node type and profile.
// Returns the requirements and true if found, or empty requirements and false if not found.
func GetRequirements(nodeType, profile string) (BaselineRequirements, bool) {
	if nodeReqs, ok := requirementsRegistry[nodeType]; ok {
		if reqs, ok := nodeReqs[profile]; ok {
			return reqs, true
		}
	}
	return BaselineRequirements{}, false
}
