// SPDX-License-Identifier: Apache-2.0

package hardware

import (
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
)

// getReqs is a test helper that looks up requirements via the provider registry.
// It mirrors the old GetRequirements(nodeType, profile) call-site pattern.
func getReqs(nodeType, profile string) (BaselineRequirements, bool) {
	providers := Providers()
	p, ok := providers[nodeType]
	if !ok {
		return BaselineRequirements{}, false
	}
	spec := DeploymentSpec{NodeType: nodeType, Profile: profile}
	reqs, err := p.Compute(spec)
	if err != nil {
		return BaselineRequirements{}, false
	}
	// A valid profile must yield a non-zero CPU floor.
	if reqs.MinCpuCores == 0 {
		return BaselineRequirements{}, false
	}
	return reqs, true
}

func TestRequirementsRegistry(t *testing.T) {
	// Consensus: every profile must return a non-zero floor.
	for _, profile := range []string{models.ProfileLocal, models.ProfilePerfnet, models.ProfileTestnet, models.ProfilePreviewnet, models.ProfileMainnet} {
		t.Run("consensus_"+profile, func(t *testing.T) {
			reqs, found := getReqs(models.NodeTypeConsensus, profile)
			if !found {
				t.Errorf("Expected requirements for consensus/%s to exist", profile)
				return
			}
			if reqs.MinCpuCores <= 0 {
				t.Errorf("Expected MinCpuCores > 0 for consensus/%s, got %d", profile, reqs.MinCpuCores)
			}
			if reqs.MinMemoryGB <= 0 {
				t.Errorf("Expected MinMemoryGB > 0 for consensus/%s, got %d", profile, reqs.MinMemoryGB)
			}
			hasStorage := reqs.MinStorageGB > 0 || reqs.MinSSDStorageGB > 0 || reqs.MinHDDStorageGB > 0
			if !hasStorage {
				t.Errorf("Expected some storage requirement for consensus/%s", profile)
			}
			if len(reqs.MinSupportedOS) == 0 {
				t.Errorf("Expected at least one supported OS for consensus/%s", profile)
			}
		})
	}

	// Block: all profiles except mainnet (no preset) must return a non-zero floor.
	// Mainnet with no preset = LFH bare metal, which is outside the scope of hardware checks
	// (lfh_count=0 means the provisioner does not manage bare-metal LFH on mainnet).
	for _, profile := range []string{models.ProfileLocal, models.ProfilePerfnet, models.ProfileTestnet, models.ProfilePreviewnet} {
		t.Run("block_"+profile, func(t *testing.T) {
			reqs, found := getReqs(models.NodeTypeBlock, profile)
			if !found {
				t.Errorf("Expected requirements for block/%s to exist", profile)
				return
			}
			if reqs.MinCpuCores <= 0 {
				t.Errorf("Expected MinCpuCores > 0 for block/%s, got %d", profile, reqs.MinCpuCores)
			}
			if reqs.MinMemoryGB <= 0 {
				t.Errorf("Expected MinMemoryGB > 0 for block/%s, got %d", profile, reqs.MinMemoryGB)
			}
			hasStorage := reqs.MinStorageGB > 0 || reqs.MinSSDStorageGB > 0 || reqs.MinHDDStorageGB > 0
			if !hasStorage {
				t.Errorf("Expected some storage requirement for block/%s", profile)
			}
			if len(reqs.MinSupportedOS) == 0 {
				t.Errorf("Expected at least one supported OS for block/%s", profile)
			}
		})
	}

	// Block mainnet with no preset → zero requirements (no-op check for bare-metal LFH).
	t.Run("block_mainnet_no_preset_is_noop", func(t *testing.T) {
		_, found := getReqs(models.NodeTypeBlock, models.ProfileMainnet)
		if found {
			t.Error("Expected block/mainnet with no preset to return zero requirements (no-op), but got non-zero")
		}
	})
}

func TestRequirementsNotFoundForInvalidInput(t *testing.T) {
	tests := []struct {
		name     string
		nodeType string
		profile  string
	}{
		{"Invalid node type", "invalid", models.ProfileMainnet},
		{"Invalid profile", models.NodeTypeBlock, "invalid"},
		{"Both invalid", "invalid", "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, found := getReqs(tt.nodeType, tt.profile)
			if found {
				t.Errorf("Expected requirements to NOT be found for node type %q and profile %q", tt.nodeType, tt.profile)
			}
		})
	}
}

func TestPreviewnetRequirementsAreHigher(t *testing.T) {
	// For consensus nodes, previewnet has higher requirements than testnet.
	// For block nodes, testnet and previewnet share the same machine shape (n2d-standard-16,
	// 16 vCPU / 64 GB) — this is intentional per the BN team (Slack 2026-06-17). Testnet
	// actually requires more local disk (5 TB LFH vs 3 TB LFH for previewnet) so no
	// previewnet-vs-testnet ordering assertion is valid for block nodes.
	previewnetReqs, _ := getReqs(models.NodeTypeConsensus, models.ProfilePreviewnet)
	testnetReqs, _ := getReqs(models.NodeTypeConsensus, models.ProfileTestnet)

	if previewnetReqs.MinCpuCores <= testnetReqs.MinCpuCores {
		t.Errorf("Expected consensus previewnet CPU (%d) > testnet CPU (%d)",
			previewnetReqs.MinCpuCores, testnetReqs.MinCpuCores)
	}
	if previewnetReqs.MinMemoryGB <= testnetReqs.MinMemoryGB {
		t.Errorf("Expected consensus previewnet memory (%d) > testnet memory (%d)",
			previewnetReqs.MinMemoryGB, testnetReqs.MinMemoryGB)
	}
	if previewnetReqs.MinStorageGB <= testnetReqs.MinStorageGB {
		t.Errorf("Expected consensus previewnet storage (%d) > testnet storage (%d)",
			previewnetReqs.MinStorageGB, testnetReqs.MinStorageGB)
	}
}

func TestLocalProfileHasMinimalRequirements(t *testing.T) {
	// Block node: compare local vs testnet (mainnet LFH is bare metal — no floor check).
	localBlockReqs, _ := getReqs(models.NodeTypeBlock, models.ProfileLocal)
	testnetBlockReqs, _ := getReqs(models.NodeTypeBlock, models.ProfileTestnet)
	if localBlockReqs.MinCpuCores >= testnetBlockReqs.MinCpuCores {
		t.Errorf("Expected block local CPU (%d) < testnet CPU (%d)",
			localBlockReqs.MinCpuCores, testnetBlockReqs.MinCpuCores)
	}
	if localBlockReqs.MinMemoryGB >= testnetBlockReqs.MinMemoryGB {
		t.Errorf("Expected block local memory (%d) < testnet memory (%d)",
			localBlockReqs.MinMemoryGB, testnetBlockReqs.MinMemoryGB)
	}

	// Consensus node: compare local vs mainnet.
	localConsensusReqs, _ := getReqs(models.NodeTypeConsensus, models.ProfileLocal)
	mainnetConsensusReqs, _ := getReqs(models.NodeTypeConsensus, models.ProfileMainnet)
	if localConsensusReqs.MinCpuCores >= mainnetConsensusReqs.MinCpuCores {
		t.Errorf("Expected consensus local CPU (%d) < mainnet CPU (%d)",
			localConsensusReqs.MinCpuCores, mainnetConsensusReqs.MinCpuCores)
	}
	if localConsensusReqs.MinMemoryGB >= mainnetConsensusReqs.MinMemoryGB {
		t.Errorf("Expected consensus local memory (%d) < mainnet memory (%d)",
			localConsensusReqs.MinMemoryGB, mainnetConsensusReqs.MinMemoryGB)
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
		{"Block node mainnet", models.NodeTypeBlock, models.ProfileMainnet, false},
		{"Block node previewnet", models.NodeTypeBlock, models.ProfilePreviewnet, false},
		{"Consensus node local", models.NodeTypeConsensus, models.ProfileLocal, false},
		// NewNodeSpec only validates that a provider exists for the node type;
		// profile validation (IsValidProfile) is the responsibility of CreateNodeSpec.
		{"Invalid node type", "invalid", models.ProfileMainnet, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := DeploymentSpec{NodeType: tt.nodeType, Profile: tt.profile}
			nodeSpec, err := NewNodeSpec(spec, mockHost)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for node type %q and profile %q", tt.nodeType, tt.profile)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if nodeSpec == nil {
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
		{"Valid block/mainnet", models.NodeTypeBlock, models.ProfileMainnet, false},
		{"Valid consensus/previewnet", models.NodeTypeConsensus, models.ProfilePreviewnet, false},
		{"Invalid node type", "invalid", models.ProfileMainnet, true},
		{"Invalid profile", models.NodeTypeBlock, "invalid", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := DeploymentSpec{NodeType: tt.nodeType, Profile: tt.profile}
			nodeSpec, err := CreateNodeSpec(spec, mockHost)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for node type %q and profile %q", tt.nodeType, tt.profile)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if nodeSpec == nil {
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
		options           map[string]any
		actualHostProfile HostProfile
		expectCPUPass     bool
		expectMemPass     bool
		expectStoragePass bool
	}{
		{
			name:              "Block node local - minimal resources should pass",
			nodeType:          models.NodeTypeBlock,
			profile:           models.ProfileLocal,
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 4, 4, 10),
			expectCPUPass:     true,
			expectMemPass:     true,
			expectStoragePass: true,
		},
		{
			name:              "Block node mainnet RFH - minimal resources should fail",
			nodeType:          models.NodeTypeBlock,
			profile:           models.ProfileMainnet,
			options:           map[string]any{"preset": "tier1-rfh"},
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 4, 4, 10),
			expectCPUPass:     false,
			expectMemPass:     false,
			expectStoragePass: false,
		},
		{
			name:              "Block node previewnet LFH - adequate resources should pass",
			nodeType:          models.NodeTypeBlock,
			profile:           models.ProfilePreviewnet,
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 20, 90, 4000), // 20 vCPU / 72 GB available (90*0.8) / 4 TB
			expectCPUPass:     true,
			expectMemPass:     true,
			expectStoragePass: true,
		},
		{
			name:              "Block node previewnet LFH - undersized host should fail",
			nodeType:          models.NodeTypeBlock,
			profile:           models.ProfilePreviewnet,
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 8, 32, 100), // well under 16/64/3000
			expectCPUPass:     false,
			expectMemPass:     false,
			expectStoragePass: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deploySpec := DeploymentSpec{NodeType: tt.nodeType, Profile: tt.profile, Options: tt.options}
			nodeSpec, err := CreateNodeSpec(deploySpec, tt.actualHostProfile)
			if err != nil {
				t.Fatalf("Failed to create spec: %v", err)
			}

			cpuErr := nodeSpec.ValidateCPU()
			if (cpuErr == nil) != tt.expectCPUPass {
				t.Errorf("CPU validation: expected pass=%v, got error=%v", tt.expectCPUPass, cpuErr)
			}

			memErr := nodeSpec.ValidateMemory()
			if (memErr == nil) != tt.expectMemPass {
				t.Errorf("Memory validation: expected pass=%v, got error=%v", tt.expectMemPass, memErr)
			}

			storageErr := nodeSpec.ValidateStorage()
			if (storageErr == nil) != tt.expectStoragePass {
				t.Errorf("Storage validation: expected pass=%v, got error=%v", tt.expectStoragePass, storageErr)
			}
		})
	}
}
