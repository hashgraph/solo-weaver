// SPDX-License-Identifier: Apache-2.0

package hardware

import (
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
)

// MockHostProfile is a testable implementation of HostProfile
type MockHostProfile struct {
	OSVendor          string
	OSVersion         string
	CPUCores          uint
	TotalMemoryGB     uint64
	AvailableMemoryGB uint64
	TotalStorageGB    uint64
	SSDStorageGB      uint64
	HDDStorageGB      uint64
	NodeRunning       bool
}

// NewMockHostProfile creates a new MockHostProfile for testing
func NewMockHostProfile(osVendor, osVersion string, cpuCores uint, memoryGB uint64, storageGB uint64) HostProfile {
	// Estimate available memory as ~80% of total
	availableMemoryGB := uint64(float64(memoryGB) * 0.8)

	return &MockHostProfile{
		OSVendor:          osVendor,
		OSVersion:         osVersion,
		CPUCores:          cpuCores,
		TotalMemoryGB:     memoryGB,
		AvailableMemoryGB: availableMemoryGB,
		TotalStorageGB:    storageGB,
		SSDStorageGB:      storageGB, // Default: all storage is SSD
		HDDStorageGB:      0,
		NodeRunning:       false,
	}
}

// NewMockHostProfileWithStorage creates a MockHostProfile with explicit SSD/HDD storage
func NewMockHostProfileWithStorage(osVendor, osVersion string, cpuCores uint, memoryGB, ssdGB, hddGB uint64) HostProfile {
	availableMemoryGB := uint64(float64(memoryGB) * 0.8)

	return &MockHostProfile{
		OSVendor:          osVendor,
		OSVersion:         osVersion,
		CPUCores:          cpuCores,
		TotalMemoryGB:     memoryGB,
		AvailableMemoryGB: availableMemoryGB,
		TotalStorageGB:    ssdGB + hddGB,
		SSDStorageGB:      ssdGB,
		HDDStorageGB:      hddGB,
		NodeRunning:       false,
	}
}

// NewMockHostProfileWithNodeStatus creates a new MockHostProfile with specific node running status
func NewMockHostProfileWithNodeStatus(osVendor, osVersion string, cpuCores uint, memoryGB uint64, storageGB uint64, nodeRunning bool) HostProfile {
	mock := NewMockHostProfile(osVendor, osVersion, cpuCores, memoryGB, storageGB).(*MockHostProfile)
	mock.NodeRunning = nodeRunning
	return mock
}

func (m *MockHostProfile) GetOSVendor() string          { return m.OSVendor }
func (m *MockHostProfile) GetOSVersion() string         { return m.OSVersion }
func (m *MockHostProfile) GetCPUCores() uint            { return m.CPUCores }
func (m *MockHostProfile) GetTotalMemoryGB() uint64     { return m.TotalMemoryGB }
func (m *MockHostProfile) GetAvailableMemoryGB() uint64 { return m.AvailableMemoryGB }
func (m *MockHostProfile) GetTotalStorageGB() uint64    { return m.TotalStorageGB }
func (m *MockHostProfile) GetSSDStorageGB() uint64      { return m.SSDStorageGB }
func (m *MockHostProfile) GetHDDStorageGB() uint64      { return m.HDDStorageGB }
func (m *MockHostProfile) String() string               { return "MockHostProfile" }

// IsNodeAlreadyRunning is a mock implementation for testing
func (m *MockHostProfile) IsNodeAlreadyRunning() bool {
	return m.NodeRunning
}

func TestIsNodeAlreadyRunning(t *testing.T) {
	tests := []struct {
		name        string
		nodeRunning bool
		expected    bool
	}{
		{
			name:        "Node is running",
			nodeRunning: true,
			expected:    true,
		},
		{
			name:        "Node is not running",
			nodeRunning: false,
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockHostProfileWithNodeStatus("ubuntu", "20.04", 8, 16, 500, tt.nodeRunning)

			result := mock.IsNodeAlreadyRunning()
			if result != tt.expected {
				t.Errorf("Expected IsNodeAlreadyRunning() to return %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestNodeSpecValidationWithRunningNode(t *testing.T) {
	tests := []struct {
		name             string
		nodeType         string
		profile          string
		options          map[string]any
		hostProfile      HostProfile
		nodeRunning      bool
		expectValidation bool
		description      string
	}{
		{
			name:             "Block node local validation when node is not running",
			nodeType:         models.NodeTypeBlock,
			profile:          models.ProfileLocal,
			hostProfile:      NewMockHostProfileWithNodeStatus("ubuntu", "20.04", 4, 4, 10, false),
			nodeRunning:      false,
			expectValidation: true,
			description:      "Should validate successfully when no node is running",
		},
		{
			name:             "Block node local validation when node is already running",
			nodeType:         models.NodeTypeBlock,
			profile:          models.ProfileLocal,
			hostProfile:      NewMockHostProfileWithNodeStatus("ubuntu", "20.04", 4, 4, 10, true),
			nodeRunning:      true,
			expectValidation: true,
			description:      "Should still validate hardware requirements even if node is running",
		},
		{
			name:             "Block node mainnet RFH validation when node is not running",
			nodeType:         models.NodeTypeBlock,
			profile:          models.ProfileMainnet,
			options:          map[string]any{"preset": "tier1-rfh"},
			hostProfile:      NewMockHostProfileWithNodeStatus("ubuntu", "20.04", 36, 322, 200, false), // 36 cores, 257GB available (322*0.8), 200GB storage
			nodeRunning:      false,
			expectValidation: true,
			description:      "Should validate successfully when no node is running",
		},
		{
			name:             "Consensus node mainnet validation when node is already running",
			nodeType:         models.NodeTypeConsensus,
			profile:          models.ProfileMainnet,
			hostProfile:      NewMockHostProfileWithNodeStatus("ubuntu", "20.04", 48, 322, 9000, true), // 48 cores, 256GB+ available, 8TB+
			nodeRunning:      true,
			expectValidation: true,
			description:      "Should still validate hardware requirements even if node is running",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := DeploymentSpec{NodeType: tt.nodeType, Profile: tt.profile, Options: tt.options}
			nodeSpec, err := NewNodeSpec(spec, tt.hostProfile)
			if err != nil {
				t.Fatalf("Failed to create spec: %v", err)
			}

			// Test that IsNodeAlreadyRunning returns the expected value
			if tt.hostProfile.IsNodeAlreadyRunning() != tt.nodeRunning {
				t.Errorf("Expected IsNodeAlreadyRunning() to return %v, got %v",
					tt.nodeRunning, tt.hostProfile.IsNodeAlreadyRunning())
			}

			// Test that hardware validation still works regardless of node running status
			osErr := nodeSpec.ValidateOS()
			cpuErr := nodeSpec.ValidateCPU()
			memErr := nodeSpec.ValidateMemory()
			storageErr := nodeSpec.ValidateStorage()

			if tt.expectValidation {
				if osErr != nil {
					t.Errorf("OS validation failed: %v", osErr)
				}
				if cpuErr != nil {
					t.Errorf("CPU validation failed: %v", cpuErr)
				}
				if memErr != nil {
					t.Errorf("Memory validation failed: %v", memErr)
				}
				if storageErr != nil {
					t.Errorf("Storage validation failed: %v", storageErr)
				}
			}
		})
	}
}

func TestMockHostProfile(t *testing.T) {
	// Test MockHostProfile implementation
	mock := NewMockHostProfile("ubuntu", "20.04", 8, 16, 500)

	if mock.GetOSVendor() != "ubuntu" {
		t.Errorf("Expected OS vendor 'ubuntu', got '%s'", mock.GetOSVendor())
	}

	if mock.GetOSVersion() != "20.04" {
		t.Errorf("Expected OS version '20.04', got '%s'", mock.GetOSVersion())
	}

	if mock.GetCPUCores() != 8 {
		t.Errorf("Expected 8 CPU cores, got %d", mock.GetCPUCores())
	}

	expectedMemory := uint64(16)
	if mock.GetTotalMemoryGB() != expectedMemory {
		t.Errorf("Expected %d GB of memory, got %d", expectedMemory, mock.GetTotalMemoryGB())
	}

	if mock.GetTotalStorageGB() != 500 {
		t.Errorf("Expected 500 GB storage, got %d", mock.GetTotalStorageGB())
	}

	// Test that default node running status is false
	if mock.IsNodeAlreadyRunning() != false {
		t.Errorf("Expected IsNodeAlreadyRunning() to return false by default, got %v", mock.IsNodeAlreadyRunning())
	}
}

func TestNodeSpecWithMockHostProfile(t *testing.T) {
	tests := []struct {
		name              string
		nodeType          string
		profile           string
		options           map[string]any
		actualHostProfile HostProfile
		expectedOS        bool
		expectedCPU       bool
		expectedMem       bool
		expectedStorage   bool
	}{
		{
			name:              "Block node local with sufficient resources",
			nodeType:          models.NodeTypeBlock,
			profile:           models.ProfileLocal,
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 4, 4, 600),
			expectedOS:        true,
			expectedCPU:       true,
			expectedMem:       true,
			expectedStorage:   true,
		},
		{
			name:              "Block node local with insufficient CPU",
			nodeType:          models.NodeTypeBlock,
			profile:           models.ProfileLocal,
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 0, 4, 600),
			expectedOS:        true,
			expectedCPU:       false,
			expectedMem:       true,
			expectedStorage:   true,
		},
		{
			name:              "Block node mainnet RFH with sufficient resources",
			nodeType:          models.NodeTypeBlock,
			profile:           models.ProfileMainnet,
			options:           map[string]any{"preset": "tier1-rfh"},
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 36, 322, 200), // 36 cores, 257GB available (322*0.8), 200GB storage
			expectedOS:        true,
			expectedCPU:       true,
			expectedMem:       true,
			expectedStorage:   true,
		},
		{
			name:              "Block node mainnet RFH with insufficient memory",
			nodeType:          models.NodeTypeBlock,
			profile:           models.ProfileMainnet,
			options:           map[string]any{"preset": "tier1-rfh"},
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 36, 32, 200), // sufficient CPU (36>=32) and storage, insufficient memory (25GB available < 256GB)
			expectedOS:        true,
			expectedCPU:       true,
			expectedMem:       false,
			expectedStorage:   true,
		},
		{
			name:              "Consensus node mainnet with sufficient resources",
			nodeType:          models.NodeTypeConsensus,
			profile:           models.ProfileMainnet,
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 48, 322, 9000), // 48 cores, 256GB+ available (322*0.8=257), 8TB+
			expectedOS:        true,
			expectedCPU:       true,
			expectedMem:       true,
			expectedStorage:   true,
		},
		{
			name:              "Consensus node mainnet with insufficient storage",
			nodeType:          models.NodeTypeConsensus,
			profile:           models.ProfileMainnet,
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 48, 322, 5000), // sufficient CPU/mem, insufficient storage (need 8TB)
			expectedOS:        true,
			expectedCPU:       true,
			expectedMem:       true,
			expectedStorage:   false,
		},
		{
			name:              "Block node local with unsupported OS",
			nodeType:          models.NodeTypeBlock,
			profile:           models.ProfileLocal,
			actualHostProfile: NewMockHostProfile("windows", "10", 4, 4, 600),
			expectedOS:        false,
			expectedCPU:       true,
			expectedMem:       true,
			expectedStorage:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := DeploymentSpec{NodeType: tt.nodeType, Profile: tt.profile, Options: tt.options}
			nodeSpec, err := NewNodeSpec(spec, tt.actualHostProfile)
			if err != nil {
				t.Fatalf("Failed to create spec: %v", err)
			}

			osErr := nodeSpec.ValidateOS()
			if (osErr == nil) != tt.expectedOS {
				t.Errorf("OS validation expected %v, got error: %v", tt.expectedOS, osErr)
			}

			cpuErr := nodeSpec.ValidateCPU()
			if (cpuErr == nil) != tt.expectedCPU {
				t.Errorf("CPU validation expected %v, got error: %v", tt.expectedCPU, cpuErr)
			}

			memErr := nodeSpec.ValidateMemory()
			if (memErr == nil) != tt.expectedMem {
				t.Errorf("Memory validation expected %v, got error: %v", tt.expectedMem, memErr)
			}

			storageErr := nodeSpec.ValidateStorage()
			if (storageErr == nil) != tt.expectedStorage {
				t.Errorf("Storage validation expected %v, got error: %v", tt.expectedStorage, storageErr)
			}
		})
	}
}

func TestOSValidation(t *testing.T) {
	tests := []struct {
		name           string
		supportedOS    []string
		systemOS       string
		systemVersion  string
		expectedResult bool
	}{
		{
			name:           "Ubuntu 20 supports Ubuntu 18",
			supportedOS:    []string{"Ubuntu 18"},
			systemOS:       "ubuntu",
			systemVersion:  "20.04",
			expectedResult: true,
		},
		{
			name:           "Ubuntu 16 does not support Ubuntu 18",
			supportedOS:    []string{"Ubuntu 18"},
			systemOS:       "ubuntu",
			systemVersion:  "16.04",
			expectedResult: false,
		},
		{
			name:           "Debian 11 supports Debian 10",
			supportedOS:    []string{"Debian 10"},
			systemOS:       "debian",
			systemVersion:  "11.2",
			expectedResult: true,
		},
		{
			name:           "Multiple supported OS - one matches",
			supportedOS:    []string{"Ubuntu 18", "Debian 10"},
			systemOS:       "debian",
			systemVersion:  "11.2",
			expectedResult: true,
		},
		{
			name:           "Unsupported OS",
			supportedOS:    []string{"Ubuntu 18", "Debian 10"},
			systemOS:       "centos",
			systemVersion:  "8",
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			systemInfo := NewMockHostProfile(tt.systemOS, tt.systemVersion, 4, 2, 500)
			result := validateOS(tt.supportedOS, systemInfo)

			if result != tt.expectedResult {
				t.Errorf("OS validation expected %v, got %v", tt.expectedResult, result)
			}
		})
	}
}

func TestNodeTypeAndProfileCombinations(t *testing.T) {
	// Test that each node type + profile returns the correct display name
	mockHostProfile := NewMockHostProfile("ubuntu", "20.04", 48, 322, 9000)

	tests := []struct {
		nodeType     string
		profile      string
		expectedName string
	}{
		{models.NodeTypeBlock, models.ProfileLocal, "Block Node (Local)"},
		{models.NodeTypeBlock, models.ProfileMainnet, "Block Node (Mainnet)"},
		{models.NodeTypeBlock, models.ProfilePreviewnet, "Block Node (Previewnet)"},
		{models.NodeTypeConsensus, models.ProfileLocal, "Consensus Node (Local)"},
		{models.NodeTypeConsensus, models.ProfileMainnet, "Consensus Node (Mainnet)"},
		{models.NodeTypeConsensus, models.ProfilePreviewnet, "Consensus Node (Previewnet)"},
	}

	for _, tt := range tests {
		t.Run(tt.nodeType+"_"+tt.profile, func(t *testing.T) {
			spec := DeploymentSpec{NodeType: tt.nodeType, Profile: tt.profile}
			nodeSpec, err := NewNodeSpec(spec, mockHostProfile)
			if err != nil {
				t.Fatalf("Failed to create spec: %v", err)
			}
			if nodeSpec.GetNodeType() != tt.expectedName {
				t.Errorf("Expected node type '%s', got '%s'", tt.expectedName, nodeSpec.GetNodeType())
			}
		})
	}
}

func TestPreviewnetNodeSpec(t *testing.T) {
	// Previewnet LFH (no preset): n2d-standard-16 → 16 cores / 64 GB / 3000 GB.
	tests := []struct {
		name              string
		actualHostProfile HostProfile
		expectedOS        bool
		expectedCPU       bool
		expectedMem       bool
		expectedStorage   bool
	}{
		{
			name:              "Previewnet block node with sufficient resources",
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 20, 90, 4000), // 20 cores, 72GB available (90*0.8), 4TB
			expectedOS:        true,
			expectedCPU:       true,
			expectedMem:       true,
			expectedStorage:   true,
		},
		{
			name:              "Previewnet block node with insufficient CPU",
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 12, 90, 4000), // 12 < 16
			expectedOS:        true,
			expectedCPU:       false,
			expectedMem:       true,
			expectedStorage:   true,
		},
		{
			name:              "Previewnet block node with insufficient memory",
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 20, 50, 4000), // 40GB available (50*0.8) < 64GB
			expectedOS:        true,
			expectedCPU:       true,
			expectedMem:       false,
			expectedStorage:   true,
		},
		{
			name:              "Previewnet block node with insufficient storage",
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 20, 90, 2000), // 2000 < 3000 GB
			expectedOS:        true,
			expectedCPU:       true,
			expectedMem:       true,
			expectedStorage:   false,
		},
		{
			name:              "Previewnet block node barely under storage floor",
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 20, 90, 2999), // 2999 < 3000 GB
			expectedOS:        true,
			expectedCPU:       true,
			expectedMem:       true,
			expectedStorage:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := DeploymentSpec{NodeType: models.NodeTypeBlock, Profile: models.ProfilePreviewnet}
			nodeSpec, err := NewNodeSpec(spec, tt.actualHostProfile)
			if err != nil {
				t.Fatalf("Failed to create spec: %v", err)
			}

			osErr := nodeSpec.ValidateOS()
			if (osErr == nil) != tt.expectedOS {
				t.Errorf("OS validation expected %v, got error: %v", tt.expectedOS, osErr)
			}

			cpuErr := nodeSpec.ValidateCPU()
			if (cpuErr == nil) != tt.expectedCPU {
				t.Errorf("CPU validation expected %v, got error: %v", tt.expectedCPU, cpuErr)
			}

			memErr := nodeSpec.ValidateMemory()
			if (memErr == nil) != tt.expectedMem {
				t.Errorf("Memory validation expected %v, got error: %v", tt.expectedMem, memErr)
			}

			storageErr := nodeSpec.ValidateStorage()
			if (storageErr == nil) != tt.expectedStorage {
				t.Errorf("Storage validation expected %v, got error: %v", tt.expectedStorage, storageErr)
			}
		})
	}
}

func TestPreviewnetNodeRequirements(t *testing.T) {
	// Previewnet LFH (no preset): n2d-standard-16 → 16 cores / 64 GB / 3000 GB total.
	mockHostProfile := NewMockHostProfile("ubuntu", "20.04", 20, 90, 4000)
	spec := DeploymentSpec{NodeType: models.NodeTypeBlock, Profile: models.ProfilePreviewnet}
	nodeSpec, err := NewNodeSpec(spec, mockHostProfile)
	if err != nil {
		t.Fatalf("Failed to create spec: %v", err)
	}

	requirements := nodeSpec.GetBaselineRequirements()

	if requirements.MinCpuCores != 16 {
		t.Errorf("Expected 16 CPU cores, got %d", requirements.MinCpuCores)
	}
	if requirements.MinMemoryGB != 64 {
		t.Errorf("Expected 64 GB memory, got %d", requirements.MinMemoryGB)
	}
	if requirements.MinStorageGB != 3000 {
		t.Errorf("Expected 3000 GB storage, got %d", requirements.MinStorageGB)
	}
}
