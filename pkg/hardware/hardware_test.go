// SPDX-License-Identifier: Apache-2.0

package hardware

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/core"
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
		hostProfile      HostProfile
		nodeRunning      bool
		expectValidation bool
		description      string
	}{
		{
			name:             "Block node local validation when node is not running",
			nodeType:         core.NodeTypeBlock,
			profile:          core.ProfileLocal,
			hostProfile:      NewMockHostProfileWithNodeStatus("ubuntu", "20.04", 4, 4, 10, false),
			nodeRunning:      false,
			expectValidation: true,
			description:      "Should validate successfully when no node is running",
		},
		{
			name:             "Block node local validation when node is already running",
			nodeType:         core.NodeTypeBlock,
			profile:          core.ProfileLocal,
			hostProfile:      NewMockHostProfileWithNodeStatus("ubuntu", "20.04", 4, 4, 10, true),
			nodeRunning:      true,
			expectValidation: true,
			description:      "Should still validate hardware requirements even if node is running",
		},
		{
			name:             "Block node mainnet validation when node is not running",
			nodeType:         core.NodeTypeBlock,
			profile:          core.ProfileMainnet,
			hostProfile:      NewMockHostProfileWithNodeStatus("ubuntu", "20.04", 8, 22, 6000, false), // 8 cores, 16GB+ available, 5TB+
			nodeRunning:      false,
			expectValidation: true,
			description:      "Should validate successfully when no node is running",
		},
		{
			name:             "Consensus node mainnet validation when node is already running",
			nodeType:         core.NodeTypeConsensus,
			profile:          core.ProfileMainnet,
			hostProfile:      NewMockHostProfileWithNodeStatus("ubuntu", "20.04", 48, 322, 9000, true), // 48 cores, 256GB+ available, 8TB+
			nodeRunning:      true,
			expectValidation: true,
			description:      "Should still validate hardware requirements even if node is running",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := NewNodeSpec(tt.nodeType, tt.profile, tt.hostProfile)
			if err != nil {
				t.Fatalf("Failed to create spec: %v", err)
			}

			// Test that IsNodeAlreadyRunning returns the expected value
			if tt.hostProfile.IsNodeAlreadyRunning() != tt.nodeRunning {
				t.Errorf("Expected IsNodeAlreadyRunning() to return %v, got %v",
					tt.nodeRunning, tt.hostProfile.IsNodeAlreadyRunning())
			}

			// Test that hardware validation still works regardless of node running status
			osErr := spec.ValidateOS()
			cpuErr := spec.ValidateCPU()
			memErr := spec.ValidateMemory()
			storageErr := spec.ValidateStorage()

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
		actualHostProfile HostProfile
		expectedOS        bool
		expectedCPU       bool
		expectedMem       bool
		expectedStorage   bool
	}{
		{
			name:              "Block node local with sufficient resources",
			nodeType:          core.NodeTypeBlock,
			profile:           core.ProfileLocal,
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 4, 4, 600),
			expectedOS:        true,
			expectedCPU:       true,
			expectedMem:       true,
			expectedStorage:   true,
		},
		{
			name:              "Block node local with insufficient CPU",
			nodeType:          core.NodeTypeBlock,
			profile:           core.ProfileLocal,
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 0, 4, 600),
			expectedOS:        true,
			expectedCPU:       false,
			expectedMem:       true,
			expectedStorage:   true,
		},
		{
			name:              "Block node mainnet with sufficient resources",
			nodeType:          core.NodeTypeBlock,
			profile:           core.ProfileMainnet,
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 8, 22, 6000),
			expectedOS:        true,
			expectedCPU:       true,
			expectedMem:       true,
			expectedStorage:   true,
		},
		{
			name:              "Block node mainnet with insufficient memory",
			nodeType:          core.NodeTypeBlock,
			profile:           core.ProfileMainnet,
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 8, 8, 6000),
			expectedOS:        true,
			expectedCPU:       true,
			expectedMem:       false,
			expectedStorage:   true,
		},
		{
			name:              "Consensus node mainnet with sufficient resources",
			nodeType:          core.NodeTypeConsensus,
			profile:           core.ProfileMainnet,
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 48, 322, 9000), // 48 cores, 256GB+ available (322*0.8=257), 8TB+
			expectedOS:        true,
			expectedCPU:       true,
			expectedMem:       true,
			expectedStorage:   true,
		},
		{
			name:              "Consensus node mainnet with insufficient storage",
			nodeType:          core.NodeTypeConsensus,
			profile:           core.ProfileMainnet,
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 48, 322, 5000), // sufficient CPU/mem, insufficient storage (need 8TB)
			expectedOS:        true,
			expectedCPU:       true,
			expectedMem:       true,
			expectedStorage:   false,
		},
		{
			name:              "Block node local with unsupported OS",
			nodeType:          core.NodeTypeBlock,
			profile:           core.ProfileLocal,
			actualHostProfile: NewMockHostProfile("windows", "10", 4, 4, 600),
			expectedOS:        false,
			expectedCPU:       true,
			expectedMem:       true,
			expectedStorage:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := NewNodeSpec(tt.nodeType, tt.profile, tt.actualHostProfile)
			if err != nil {
				t.Fatalf("Failed to create spec: %v", err)
			}

			osErr := spec.ValidateOS()
			if (osErr == nil) != tt.expectedOS {
				t.Errorf("OS validation expected %v, got error: %v", tt.expectedOS, osErr)
			}

			cpuErr := spec.ValidateCPU()
			if (cpuErr == nil) != tt.expectedCPU {
				t.Errorf("CPU validation expected %v, got error: %v", tt.expectedCPU, cpuErr)
			}

			memErr := spec.ValidateMemory()
			if (memErr == nil) != tt.expectedMem {
				t.Errorf("Memory validation expected %v, got error: %v", tt.expectedMem, memErr)
			}

			storageErr := spec.ValidateStorage()
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
		{core.NodeTypeBlock, core.ProfileLocal, "Block Node (Local)"},
		{core.NodeTypeBlock, core.ProfileMainnet, "Block Node (Mainnet)"},
		{core.NodeTypeBlock, core.ProfilePreviewnet, "Block Node (Previewnet)"},
		{core.NodeTypeConsensus, core.ProfileLocal, "Consensus Node (Local)"},
		{core.NodeTypeConsensus, core.ProfileMainnet, "Consensus Node (Mainnet)"},
		{core.NodeTypeConsensus, core.ProfilePreviewnet, "Consensus Node (Previewnet)"},
	}

	for _, tt := range tests {
		t.Run(tt.nodeType+"_"+tt.profile, func(t *testing.T) {
			spec, err := NewNodeSpec(tt.nodeType, tt.profile, mockHostProfile)
			if err != nil {
				t.Fatalf("Failed to create spec: %v", err)
			}
			if spec.GetNodeType() != tt.expectedName {
				t.Errorf("Expected node type '%s', got '%s'", tt.expectedName, spec.GetNodeType())
			}
		})
	}
}

func TestPreviewnetNodeSpec(t *testing.T) {
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
			actualHostProfile: NewMockHostProfileWithStorage("ubuntu", "20.04", 48, 322, 9000, 25000), // 9TB SSD, 25TB HDD
			expectedOS:        true,
			expectedCPU:       true,
			expectedMem:       true,
			expectedStorage:   true,
		},
		{
			name:              "Previewnet block node with insufficient CPU",
			actualHostProfile: NewMockHostProfileWithStorage("ubuntu", "20.04", 32, 322, 9000, 25000),
			expectedOS:        true,
			expectedCPU:       false,
			expectedMem:       true,
			expectedStorage:   true,
		},
		{
			name:              "Previewnet block node with insufficient memory",
			actualHostProfile: NewMockHostProfileWithStorage("ubuntu", "20.04", 48, 128, 9000, 25000),
			expectedOS:        true,
			expectedCPU:       true,
			expectedMem:       false,
			expectedStorage:   true,
		},
		{
			name:              "Previewnet block node with insufficient SSD",
			actualHostProfile: NewMockHostProfileWithStorage("ubuntu", "20.04", 48, 322, 5000, 25000), // Only 5TB SSD
			expectedOS:        true,
			expectedCPU:       true,
			expectedMem:       true,
			expectedStorage:   false,
		},
		{
			name:              "Previewnet block node with insufficient HDD",
			actualHostProfile: NewMockHostProfileWithStorage("ubuntu", "20.04", 48, 322, 9000, 10000), // Only 10TB HDD
			expectedOS:        true,
			expectedCPU:       true,
			expectedMem:       true,
			expectedStorage:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := NewNodeSpec(core.NodeTypeBlock, core.ProfilePreviewnet, tt.actualHostProfile)
			if err != nil {
				t.Fatalf("Failed to create spec: %v", err)
			}

			osErr := spec.ValidateOS()
			if (osErr == nil) != tt.expectedOS {
				t.Errorf("OS validation expected %v, got error: %v", tt.expectedOS, osErr)
			}

			cpuErr := spec.ValidateCPU()
			if (cpuErr == nil) != tt.expectedCPU {
				t.Errorf("CPU validation expected %v, got error: %v", tt.expectedCPU, cpuErr)
			}

			memErr := spec.ValidateMemory()
			if (memErr == nil) != tt.expectedMem {
				t.Errorf("Memory validation expected %v, got error: %v", tt.expectedMem, memErr)
			}

			storageErr := spec.ValidateStorage()
			if (storageErr == nil) != tt.expectedStorage {
				t.Errorf("Storage validation expected %v, got error: %v", tt.expectedStorage, storageErr)
			}
		})
	}
}

func TestPreviewnetNodeRequirements(t *testing.T) {
	mockHostProfile := NewMockHostProfileWithStorage("ubuntu", "20.04", 48, 322, 9000, 25000)
	spec, err := NewNodeSpec(core.NodeTypeBlock, core.ProfilePreviewnet, mockHostProfile)
	if err != nil {
		t.Fatalf("Failed to create spec: %v", err)
	}

	requirements := spec.GetBaselineRequirements()

	// Verify previewnet requirements: 48 cores, 256GB RAM, 8TB SSD, 24TB HDD
	if requirements.MinCpuCores != 48 {
		t.Errorf("Expected 48 CPU cores, got %d", requirements.MinCpuCores)
	}
	if requirements.MinMemoryGB != 256 {
		t.Errorf("Expected 256 GB memory, got %d", requirements.MinMemoryGB)
	}
	if requirements.MinSSDStorageGB != 8000 {
		t.Errorf("Expected 8000 GB SSD storage, got %d", requirements.MinSSDStorageGB)
	}
	if requirements.MinHDDStorageGB != 24000 {
		t.Errorf("Expected 24000 GB HDD storage, got %d", requirements.MinHDDStorageGB)
	}
}
