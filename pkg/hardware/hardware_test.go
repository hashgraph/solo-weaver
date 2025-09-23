package hardware

import (
	"testing"
)

// MockHostProfile is a testable implementation of HostProfile
type MockHostProfile struct {
	OSVendor          string
	OSVersion         string
	CPUCores          uint
	TotalMemoryGB     uint64
	AvailableMemoryGB uint64
	TotalStorageGB    uint64
	NodeRunning       bool // Add this field to control the mock behavior
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
		NodeRunning:       false, // Default to false
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
		createSpec       func(HostProfile) Spec
		nodeRunning      bool
		expectValidation bool
		description      string
	}{
		{
			name:             "Local node validation when node is not running",
			createSpec:       NewLocalNodeSpec,
			nodeRunning:      false,
			expectValidation: true,
			description:      "Should validate successfully when no node is running",
		},
		{
			name:             "Local node validation when node is already running",
			createSpec:       NewLocalNodeSpec,
			nodeRunning:      true,
			expectValidation: true, // Assuming validation doesn't fail just because node is running
			description:      "Should still validate hardware requirements even if node is running",
		},
		{
			name:             "Block node validation when node is not running",
			createSpec:       NewBlockNodeSpec,
			nodeRunning:      false,
			expectValidation: true,
			description:      "Should validate successfully when no node is running",
		},
		{
			name:             "Consensus node validation when node is already running",
			createSpec:       NewConsensusNodeSpec,
			nodeRunning:      true,
			expectValidation: true,
			description:      "Should still validate hardware requirements even if node is running",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock with sufficient resources for all node types
			mock := NewMockHostProfileWithNodeStatus("ubuntu", "20.04", 16, 32, 6000, tt.nodeRunning)
			spec := tt.createSpec(mock)

			// Test that IsNodeAlreadyRunning returns the expected value
			if mock.IsNodeAlreadyRunning() != tt.nodeRunning {
				t.Errorf("Expected IsNodeAlreadyRunning() to return %v, got %v",
					tt.nodeRunning, mock.IsNodeAlreadyRunning())
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
		createSpec        func(HostProfile) Spec
		actualHostProfile HostProfile
		expectedOS        bool
		expectedCPU       bool
		expectedMem       bool
		expectedStorage   bool
	}{
		{
			name:              "Local node with sufficient resources",
			createSpec:        NewLocalNodeSpec,
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 4, 4, 600),
			expectedOS:        true,
			expectedCPU:       true,
			expectedMem:       true,
			expectedStorage:   true,
		},
		{
			name:              "Local node with insufficient CPU",
			createSpec:        NewLocalNodeSpec,
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 0, 4, 600),
			expectedOS:        true,
			expectedCPU:       false,
			expectedMem:       true,
			expectedStorage:   true,
		},
		{
			name:              "Block node with sufficient resources",
			createSpec:        NewBlockNodeSpec,
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 8, 22, 6000), // Increased from 16 to 22 to account for system buffer
			expectedOS:        true,
			expectedCPU:       true,
			expectedMem:       true,
			expectedStorage:   true,
		},
		{
			name:              "Block node with insufficient memory",
			createSpec:        NewBlockNodeSpec,
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 8, 8, 6000),
			expectedOS:        true,
			expectedCPU:       true,
			expectedMem:       false,
			expectedStorage:   true,
		},
		{
			name:              "Consensus node with sufficient resources",
			createSpec:        NewConsensusNodeSpec,
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 16, 42, 1200), // Increased from 32 to 42 to account for system buffer
			expectedOS:        true,
			expectedCPU:       true,
			expectedMem:       true,
			expectedStorage:   true,
		},
		{
			name:              "Consensus node with insufficient storage",
			createSpec:        NewConsensusNodeSpec,
			actualHostProfile: NewMockHostProfile("ubuntu", "20.04", 16, 42, 500), // Increased from 32 to 42 to account for system buffer
			expectedOS:        true,
			expectedCPU:       true,
			expectedMem:       true,
			expectedStorage:   false,
		},
		{
			name:              "Node with unsupported OS",
			createSpec:        NewLocalNodeSpec,
			actualHostProfile: NewMockHostProfile("windows", "10", 4, 4, 600),
			expectedOS:        false,
			expectedCPU:       true,
			expectedMem:       true,
			expectedStorage:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := tt.createSpec(tt.actualHostProfile)

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

func TestIndividualNodeTypes(t *testing.T) {
	// Test that each node type returns the correct node type string
	mockHostProfile := NewMockHostProfile("ubuntu", "20.04", 16, 32, 1200)

	localSpec := NewLocalNodeSpec(mockHostProfile)
	if localSpec.GetNodeType() != "Local Node" {
		t.Errorf("Expected Local Node type, got '%s'", localSpec.GetNodeType())
	}

	blockSpec := NewBlockNodeSpec(mockHostProfile)
	if blockSpec.GetNodeType() != "Block Node" {
		t.Errorf("Expected Block Node type, got '%s'", blockSpec.GetNodeType())
	}

	consensusSpec := NewConsensusNodeSpec(mockHostProfile)
	if consensusSpec.GetNodeType() != "Consensus Node" {
		t.Errorf("Expected Consensus Node type, got '%s'", consensusSpec.GetNodeType())
	}
}
