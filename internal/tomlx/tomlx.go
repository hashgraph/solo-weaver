// Package tomlx provides utilities for managing TOML configuration files.
//
// This package offers a TomlConfigManager that can:
// - Read and update existing TOML files while preserving structure
// - Merge configuration maps recursively
// - Set nested configuration values using dot notation paths
// - Validate TOML configurations against expected values
//
// It is primarily used by software installers to patch configuration files
// with sandbox-specific paths while maintaining the original structure.
package tomlx

import (
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// TomlConfigManager provides utilities for managing TOML configuration files
type TomlConfigManager struct{}

// NewTomlConfigManager creates a new TOML configuration manager
func NewTomlConfigManager() *TomlConfigManager {
	return &TomlConfigManager{}
}

// UpdateTomlFile reads a TOML file, applies configuration updates, and writes it back
func (tcm *TomlConfigManager) UpdateTomlFile(filePath string, configUpdates map[string]interface{}) error {
	// Read the existing TOML file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	// Parse the TOML content into a generic map to preserve all existing configuration
	var config map[string]interface{}
	err = toml.Unmarshal(data, &config)
	if err != nil {
		return err
	}

	// Merge the updates into the existing config
	tcm.MergeConfigMaps(config, configUpdates)

	// Marshal back to TOML and write to file
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	encoder := toml.NewEncoder(file)
	err = encoder.Encode(config)
	if err != nil {
		return err
	}

	return nil
}

// MergeConfigMaps recursively merges the source config into the target config
func (tcm *TomlConfigManager) MergeConfigMaps(target, source map[string]interface{}) {
	for key, value := range source {
		switch val := value.(type) {
		case map[string]interface{}:
			// If the target doesn't have this key, create it
			if _, exists := target[key]; !exists {
				target[key] = make(map[string]interface{})
			}
			// If target exists and is a map, merge recursively
			if targetMap, ok := target[key].(map[string]interface{}); ok {
				tcm.MergeConfigMaps(targetMap, val)
			} else {
				// Replace if target exists but isn't a map
				target[key] = val
			}
		default:
			// Direct assignment for non-map values
			target[key] = value
		}
	}
}

// SetNestedValue safely sets nested values in a map, creating intermediate maps as needed
// The value can be a string, slice, or any other type
func (tcm *TomlConfigManager) SetNestedValue(config map[string]interface{}, path string, value interface{}) {
	keys := strings.Split(path, ".")
	m := config

	// Navigate/create the nested structure
	for i := 0; i < len(keys)-1; i++ {
		key := keys[i]
		if _, exists := m[key]; !exists {
			m[key] = make(map[string]interface{})
		}
		// Type assertion to continue navigating
		if nextMap, ok := m[key].(map[string]interface{}); ok {
			m = nextMap
		} else {
			// If the existing value isn't a map, replace it
			newMap := make(map[string]interface{})
			m[key] = newMap
			m = newMap
		}
	}
	// Set the final value
	m[keys[len(keys)-1]] = value
}

// ValidateConfigValues recursively validates that the actual TOML config matches the expected config
func (tcm *TomlConfigManager) ValidateConfigValues(actual map[string]any, expected map[string]interface{}) bool {
	return tcm.validateConfigMap(actual, expected, "")
}

// validateConfigMap recursively compares actual vs expected configuration maps
func (tcm *TomlConfigManager) validateConfigMap(actual map[string]any, expected map[string]interface{}, pathPrefix string) bool {
	for key, expectedValue := range expected {
		currentPath := tcm.buildCurrentPath(pathPrefix, key)

		actualValue, exists := actual[key]
		if !exists {
			return false
		}

		if !tcm.validateSingleConfigValue(actualValue, expectedValue, currentPath) {
			return false
		}
	}
	return true
}

// buildCurrentPath constructs the current configuration path for debugging
func (tcm *TomlConfigManager) buildCurrentPath(pathPrefix, key string) string {
	if pathPrefix == "" {
		return key
	}
	return pathPrefix + "." + key
}

// validateSingleConfigValue validates a single configuration value against expected value
func (tcm *TomlConfigManager) validateSingleConfigValue(actualValue any, expectedValue interface{}, currentPath string) bool {
	switch expectedVal := expectedValue.(type) {
	case map[string]interface{}:
		return tcm.validateNestedMap(actualValue, expectedVal, currentPath)
	case []interface{}:
		return tcm.validateArray(actualValue, expectedVal)
	default:
		return actualValue == expectedValue
	}
}

// validateNestedMap validates nested configuration maps
func (tcm *TomlConfigManager) validateNestedMap(actualValue any, expectedVal map[string]interface{}, currentPath string) bool {
	if actualMap, ok := actualValue.(map[string]interface{}); ok {
		return tcm.validateConfigMap(actualMap, expectedVal, currentPath)
	}
	return false
}

// validateArray validates configuration arrays
func (tcm *TomlConfigManager) validateArray(actualValue any, expectedVal []interface{}) bool {
	actualArray, ok := actualValue.([]interface{})
	if !ok {
		return false
	}

	if len(actualArray) != len(expectedVal) {
		return false
	}

	for i, expectedItem := range expectedVal {
		if i >= len(actualArray) || actualArray[i] != expectedItem {
			return false
		}
	}
	return true
}
