// SPDX-License-Identifier: Apache-2.0

package hardware

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/core"
)

func TestRequirementsRegistry(t *testing.T) {
	// Test that all expected combinations exist in the registry
	nodeTypes := []string{core.NodeTypeBlock, core.NodeTypeConsensus}
	profiles := []string{core.ProfileLocal, core.ProfilePerfnet, core.ProfileTestnet, core.ProfilePreviewnet, core.ProfileMainnet}

	for _, nodeType := range nodeTypes {
		for _, profile := range profiles {
			t.Run(nodeType+"_"+profile, func(t *testing.T) {
				reqs, found := GetRequirements(nodeType, profile)
				if !found {
					t.Errorf("Expected requirements for node type %q and profile %q to exist", nodeType, profile)
				}
				// Basic sanity checks
				if reqs.MinCpuCores <= 0 {
					t.Errorf("Expected MinCpuCores > 0 for %s/%s, got %d", nodeType, profile, reqs.MinCpuCores)
				}
				if reqs.MinMemoryGB <= 0 {
					t.Errorf("Expected MinMemoryGB > 0 for %s/%s, got %d", nodeType, profile, reqs.MinMemoryGB)
				}
				// Storage can be either total or SSD+HDD
				hasStorage := reqs.MinStorageGB > 0 || reqs.MinSSDStorageGB > 0 || reqs.MinHDDStorageGB > 0
				if !hasStorage {
					t.Errorf("Expected some storage requirement for %s/%s", nodeType, profile)
				}
				if len(reqs.MinSupportedOS) == 0 {
					t.Errorf("Expected at least one supported OS for %s/%s", nodeType, profile)
				}
			})
		}
	}
}

func TestRequirementsNotFoundForInvalidInput(t *testing.T) {
	tests := []struct {
		name     string
		nodeType string
		profile  string
	}{
		{"Invalid node type", "invalid", core.ProfileMainnet},
		{"Invalid profile", core.NodeTypeBlock, "invalid"},
		{"Both invalid", "invalid", "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, found := GetRequirements(tt.nodeType, tt.profile)
			if found {
				t.Errorf("Expected requirements to NOT be found for node type %q and profile %q", tt.nodeType, tt.profile)
			}
		})
	}
}

func TestPreviewnetRequirementsAreHigher(t *testing.T) {
	// Previewnet should have higher requirements than other profiles
	for _, nodeType := range []string{core.NodeTypeBlock, core.NodeTypeConsensus} {
		previewnetReqs, _ := GetRequirements(nodeType, core.ProfilePreviewnet)
		testnetReqs, _ := GetRequirements(nodeType, core.ProfileTestnet)

		if previewnetReqs.MinCpuCores <= testnetReqs.MinCpuCores {
			t.Errorf("Expected previewnet CPU cores (%d) > testnet CPU cores (%d) for %s",
				previewnetReqs.MinCpuCores, testnetReqs.MinCpuCores, nodeType)
		}
		if previewnetReqs.MinMemoryGB <= testnetReqs.MinMemoryGB {
			t.Errorf("Expected previewnet memory (%d) > testnet memory (%d) for %s",
				previewnetReqs.MinMemoryGB, testnetReqs.MinMemoryGB, nodeType)
		}

		// For block nodes, previewnet uses SSD+HDD; for others, compare total storage
		if nodeType == core.NodeTypeBlock {
			// Block node previewnet uses SSD+HDD split
			totalPreviewnet := previewnetReqs.MinSSDStorageGB + previewnetReqs.MinHDDStorageGB
			if totalPreviewnet <= testnetReqs.MinStorageGB {
				t.Errorf("Expected previewnet total storage (%d) > testnet storage (%d) for %s",
					totalPreviewnet, testnetReqs.MinStorageGB, nodeType)
			}
		} else {
			if previewnetReqs.MinStorageGB <= testnetReqs.MinStorageGB {
				t.Errorf("Expected previewnet storage (%d) > testnet storage (%d) for %s",
					previewnetReqs.MinStorageGB, testnetReqs.MinStorageGB, nodeType)
			}
		}
	}
}

func TestLocalProfileHasMinimalRequirements(t *testing.T) {
	// Local profile should have minimal requirements for development
	for _, nodeType := range []string{core.NodeTypeBlock, core.NodeTypeConsensus} {
		localReqs, _ := GetRequirements(nodeType, core.ProfileLocal)
		mainnetReqs, _ := GetRequirements(nodeType, core.ProfileMainnet)

		if localReqs.MinCpuCores >= mainnetReqs.MinCpuCores {
			t.Errorf("Expected local CPU cores (%d) < mainnet CPU cores (%d) for %s",
				localReqs.MinCpuCores, mainnetReqs.MinCpuCores, nodeType)
		}
		if localReqs.MinMemoryGB >= mainnetReqs.MinMemoryGB {
			t.Errorf("Expected local memory (%d) < mainnet memory (%d) for %s",
				localReqs.MinMemoryGB, mainnetReqs.MinMemoryGB, nodeType)
		}
	}
}

func TestNewNodeSpecWithRegistry(t *testing.T) {
	mockHost := NewMockHostProfile("ubuntu", "20.04", 48, 322, 9000)

	tests := []struct {
		name        string
		nodeType    string
		profile     string
		expectError bool
	}{
		{"Block node mainnet", core.NodeTypeBlock, core.ProfileMainnet, false},
		{"Block node previewnet", core.NodeTypeBlock, core.ProfilePreviewnet, false},
		{"Consensus node local", core.NodeTypeConsensus, core.ProfileLocal, false},
		{"Invalid node type", "invalid", core.ProfileMainnet, true},
		{"Invalid profile", core.NodeTypeBlock, "invalid", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := NewNodeSpec(tt.nodeType, tt.profile, mockHost)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for node type %q and profile %q", tt.nodeType, tt.profile)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if spec == nil {
					t.Error("Expected non-nil spec")
				}
			}
		})
	}
}

func TestCreateNodeSpecValidation(t *testing.T) {
	mockHost := NewMockHostProfile("ubuntu", "20.04", 48, 322, 9000)

	tests := []struct {
		name        string
		nodeType    string
		profile     string
		expectError bool
	}{
		{"Valid block/mainnet", core.NodeTypeBlock, core.ProfileMainnet, false},
		{"Valid consensus/previewnet", core.NodeTypeConsensus, core.ProfilePreviewnet, false},
		{"Invalid node type", "invalid", core.ProfileMainnet, true},
		{"Invalid profile", core.NodeTypeBlock, "invalid", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := CreateNodeSpec(tt.nodeType, tt.profile, mockHost)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for node type %q and profile %q", tt.nodeType, tt.profile)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if spec == nil {
					t.Error("Expected non-nil spec")
				}
			}
		})
	}
}

func TestNodeSpecValidationWithDifferentProfiles(t *testing.T) {
	// Test that the same node type with different profiles has different requirements
	tests := []struct {
		name              string
		nodeType          string
		profile           string
		actualHostProfile HostProfile
		expectCPUPass     bool
		expectMemPass     bool
		expectStoragePass bool
	}{
		{
			name:              "Block node local - minimal resources should pass",
			nodeType:          core.NodeTypeBlock,
			profile:           core.ProfileLocal,
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 4, 4, 10),
			expectCPUPass:     true,
			expectMemPass:     true,
			expectStoragePass: true,
		},
		{
			name:              "Block node mainnet - minimal resources should fail",
			nodeType:          core.NodeTypeBlock,
			profile:           core.ProfileMainnet,
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 4, 4, 10),
			expectCPUPass:     false,
			expectMemPass:     false,
			expectStoragePass: false,
		},
		{
			name:              "Block node previewnet - high resources should pass",
			nodeType:          core.NodeTypeBlock,
			profile:           core.ProfilePreviewnet,
			actualHostProfile: NewMockHostProfileWithStorage("ubuntu", "20.04", 48, 322, 9000, 25000), // 9TB SSD, 25TB HDD
			expectCPUPass:     true,
			expectMemPass:     true,
			expectStoragePass: true,
		},
		{
			name:              "Block node previewnet - medium resources should fail",
			nodeType:          core.NodeTypeBlock,
			profile:           core.ProfilePreviewnet,
			actualHostProfile: NewMockHostProfileWithStorage("ubuntu", "20.04", 16, 64, 5000, 10000), // insufficient
			expectCPUPass:     false,
			expectMemPass:     false,
			expectStoragePass: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := CreateNodeSpec(tt.nodeType, tt.profile, tt.actualHostProfile)
			if err != nil {
				t.Fatalf("Failed to create spec: %v", err)
			}

			cpuErr := spec.ValidateCPU()
			if (cpuErr == nil) != tt.expectCPUPass {
				t.Errorf("CPU validation: expected pass=%v, got error=%v", tt.expectCPUPass, cpuErr)
			}

			memErr := spec.ValidateMemory()
			if (memErr == nil) != tt.expectMemPass {
				t.Errorf("Memory validation: expected pass=%v, got error=%v", tt.expectMemPass, memErr)
			}

			storageErr := spec.ValidateStorage()
			if (storageErr == nil) != tt.expectStoragePass {
				t.Errorf("Storage validation: expected pass=%v, got error=%v", tt.expectStoragePass, storageErr)
			}
		})
	}
}
