// SPDX-License-Identifier: Apache-2.0

package tomlx

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func Test_NewTomlConfigManager(t *testing.T) {
	manager := NewTomlConfigManager()
	if manager == nil {
		t.Fatal("Expected non-nil TomlConfigManager")
	}
}

func Test_UpdateTomlFile(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.toml")

	// Test case 1: Create new TOML file
	t.Run("CreateNewFile", func(t *testing.T) {
		manager := NewTomlConfigManager()

		// Initial content
		initialContent := `[database]
host = "localhost"
port = 5432

[logging]
level = "info"`

		err := os.WriteFile(testFile, []byte(initialContent), 0644)
		if err != nil {
			t.Fatalf("Failed to write initial test file: %v", err)
		}

		// Updates to apply
		updates := map[string]interface{}{
			"database": map[string]interface{}{
				"port": 3306,
				"user": "admin",
			},
			"logging": map[string]interface{}{
				"level": "debug",
			},
			"newSection": map[string]interface{}{
				"enabled": true,
			},
		}

		err = manager.UpdateTomlFile(testFile, updates)
		if err != nil {
			t.Fatalf("UpdateTomlFile failed: %v", err)
		}

		// Verify the file was updated correctly
		data, err := os.ReadFile(testFile)
		if err != nil {
			t.Fatalf("Failed to read updated file: %v", err)
		}

		// The exact format may vary, but we can check that our values are present
		content := string(data)
		if !contains(content, "port = 3306") {
			t.Error("Expected port to be updated to 3306")
		}
		if !contains(content, "user = \"admin\"") {
			t.Error("Expected user to be added")
		}
		if !contains(content, "level = \"debug\"") {
			t.Error("Expected logging level to be updated to debug")
		}
		if !contains(content, "enabled = true") {
			t.Error("Expected newSection to be added")
		}
	})

	// Test case 2: File doesn't exist
	t.Run("FileNotExists", func(t *testing.T) {
		manager := NewTomlConfigManager()
		nonExistentFile := filepath.Join(tmpDir, "nonexistent.toml")

		updates := map[string]interface{}{
			"test": "value",
		}

		err := manager.UpdateTomlFile(nonExistentFile, updates)
		if err == nil {
			t.Error("Expected error when file doesn't exist")
		}
	})

	// Test case 3: Invalid TOML content
	t.Run("InvalidToml", func(t *testing.T) {
		manager := NewTomlConfigManager()
		invalidFile := filepath.Join(tmpDir, "invalid.toml")

		// Write invalid TOML content
		err := os.WriteFile(invalidFile, []byte("invalid toml [[["), 0644)
		if err != nil {
			t.Fatalf("Failed to write invalid test file: %v", err)
		}

		updates := map[string]interface{}{
			"test": "value",
		}

		err = manager.UpdateTomlFile(invalidFile, updates)
		if err == nil {
			t.Error("Expected error when parsing invalid TOML")
		}
	})
}

func Test_MergeConfigMaps(t *testing.T) {
	manager := NewTomlConfigManager()

	// Test case 1: Basic merge
	t.Run("BasicMerge", func(t *testing.T) {
		target := map[string]interface{}{
			"existing": "value",
			"database": map[string]interface{}{
				"host": "localhost",
				"port": 5432,
			},
		}

		source := map[string]interface{}{
			"new": "value",
			"database": map[string]interface{}{
				"port": 3306,
				"user": "admin",
			},
		}

		manager.MergeConfigMaps(target, source)

		// Check that existing values are preserved
		if target["existing"] != "value" {
			t.Error("Expected existing value to be preserved")
		}

		// Check that new values are added
		if target["new"] != "value" {
			t.Error("Expected new value to be added")
		}

		// Check that nested maps are merged correctly
		dbConfig, ok := target["database"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected database to be a map")
		}

		if dbConfig["host"] != "localhost" {
			t.Error("Expected existing nested value to be preserved")
		}
		if dbConfig["port"] != 3306 {
			t.Error("Expected nested value to be updated")
		}
		if dbConfig["user"] != "admin" {
			t.Error("Expected new nested value to be added")
		}
	})

	// Test case 2: Replace non-map with map
	t.Run("ReplaceNonMapWithMap", func(t *testing.T) {
		target := map[string]interface{}{
			"config": "simple_value",
		}

		source := map[string]interface{}{
			"config": map[string]interface{}{
				"nested": "value",
			},
		}

		manager.MergeConfigMaps(target, source)

		configMap, ok := target["config"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected config to be replaced with a map")
		}
		if configMap["nested"] != "value" {
			t.Error("Expected nested value to be set correctly")
		}
	})

	// Test case 3: Empty maps
	t.Run("EmptyMaps", func(t *testing.T) {
		target := map[string]interface{}{}
		source := map[string]interface{}{}

		manager.MergeConfigMaps(target, source)

		if len(target) != 0 {
			t.Error("Expected target to remain empty")
		}
	})
}

func Test_SetNestedValue(t *testing.T) {
	manager := NewTomlConfigManager()

	// Test case 1: Set value in new nested structure
	t.Run("SetInNewStructure", func(t *testing.T) {
		config := make(map[string]interface{})
		manager.SetNestedValue(config, "database.connection.host", "localhost")

		// Navigate to the value
		db, ok := config["database"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected database to be created as a map")
		}

		conn, ok := db["connection"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected connection to be created as a map")
		}

		if conn["host"] != "localhost" {
			t.Error("Expected host to be set to localhost")
		}
	})

	// Test case 2: Set value in existing structure
	t.Run("SetInExistingStructure", func(t *testing.T) {
		config := map[string]interface{}{
			"database": map[string]interface{}{
				"connection": map[string]interface{}{
					"port": 5432,
				},
			},
		}

		manager.SetNestedValue(config, "database.connection.host", "localhost")

		// Navigate to the value
		db := config["database"].(map[string]interface{})
		conn := db["connection"].(map[string]interface{})

		if conn["host"] != "localhost" {
			t.Error("Expected host to be added to existing structure")
		}
		if conn["port"] != 5432 {
			t.Error("Expected existing port value to be preserved")
		}
	})

	// Test case 3: Replace non-map with map in path
	t.Run("ReplaceNonMapInPath", func(t *testing.T) {
		config := map[string]interface{}{
			"database": "simple_string",
		}

		manager.SetNestedValue(config, "database.connection.host", "localhost")

		// Database should now be a map
		db, ok := config["database"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected database to be replaced with a map")
		}

		conn, ok := db["connection"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected connection to be created as a map")
		}

		if conn["host"] != "localhost" {
			t.Error("Expected host to be set correctly")
		}
	})

	// Test case 4: Single level path
	t.Run("SingleLevelPath", func(t *testing.T) {
		config := make(map[string]interface{})
		manager.SetNestedValue(config, "simple", "value")

		if config["simple"] != "value" {
			t.Error("Expected simple value to be set correctly")
		}
	})

	// Test case 5: Set array value
	t.Run("SetArrayValue", func(t *testing.T) {
		config := make(map[string]interface{})
		arrayValue := []string{"item1", "item2"}
		manager.SetNestedValue(config, "list.items", arrayValue)

		list, ok := config["list"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected list to be created as a map")
		}

		items, ok := list["items"].([]string)
		if !ok {
			t.Fatal("Expected items to be an array")
		}

		if !reflect.DeepEqual(items, arrayValue) {
			t.Error("Expected array value to be set correctly")
		}
	})
}

func Test_ValidateConfigValues(t *testing.T) {
	manager := NewTomlConfigManager()

	// Test case 1: Valid configuration
	t.Run("ValidConfiguration", func(t *testing.T) {
		actual := map[string]any{
			"database": map[string]interface{}{
				"host": "localhost",
				"port": 5432,
			},
			"logging": map[string]interface{}{
				"level": "info",
			},
		}

		expected := map[string]interface{}{
			"database": map[string]interface{}{
				"host": "localhost",
				"port": 5432,
			},
			"logging": map[string]interface{}{
				"level": "info",
			},
		}

		if !manager.ValidateConfigValues(actual, expected) {
			t.Error("Expected validation to pass for matching configurations")
		}
	})

	// Test case 2: Missing key
	t.Run("MissingKey", func(t *testing.T) {
		actual := map[string]any{
			"database": map[string]interface{}{
				"host": "localhost",
			},
		}

		expected := map[string]interface{}{
			"database": map[string]interface{}{
				"host": "localhost",
				"port": 5432,
			},
		}

		if manager.ValidateConfigValues(actual, expected) {
			t.Error("Expected validation to fail for missing key")
		}
	})

	// Test case 3: Wrong value
	t.Run("WrongValue", func(t *testing.T) {
		actual := map[string]any{
			"database": map[string]interface{}{
				"host": "localhost",
				"port": 3306,
			},
		}

		expected := map[string]interface{}{
			"database": map[string]interface{}{
				"host": "localhost",
				"port": 5432,
			},
		}

		if manager.ValidateConfigValues(actual, expected) {
			t.Error("Expected validation to fail for wrong value")
		}
	})

	// Test case 4: Array validation
	t.Run("ArrayValidation", func(t *testing.T) {
		actual := map[string]any{
			"servers": []interface{}{"server1", "server2"},
		}

		expected := map[string]interface{}{
			"servers": []interface{}{"server1", "server2"},
		}

		if !manager.ValidateConfigValues(actual, expected) {
			t.Error("Expected validation to pass for matching arrays")
		}

		// Test wrong array length
		expectedWrongLength := map[string]interface{}{
			"servers": []interface{}{"server1"},
		}

		if manager.ValidateConfigValues(actual, expectedWrongLength) {
			t.Error("Expected validation to fail for array length mismatch")
		}

		// Test wrong array content
		expectedWrongContent := map[string]interface{}{
			"servers": []interface{}{"server1", "server3"},
		}

		if manager.ValidateConfigValues(actual, expectedWrongContent) {
			t.Error("Expected validation to fail for array content mismatch")
		}
	})

	// Test case 5: Type mismatch
	t.Run("TypeMismatch", func(t *testing.T) {
		actual := map[string]any{
			"config": "string_value",
		}

		expected := map[string]interface{}{
			"config": map[string]interface{}{
				"nested": "value",
			},
		}

		if manager.ValidateConfigValues(actual, expected) {
			t.Error("Expected validation to fail for type mismatch")
		}
	})

	// Test case 6: Empty configurations
	t.Run("EmptyConfigurations", func(t *testing.T) {
		actual := map[string]any{}
		expected := map[string]interface{}{}

		if !manager.ValidateConfigValues(actual, expected) {
			t.Error("Expected validation to pass for empty configurations")
		}
	})
}

func Test_BuildCurrentPath(t *testing.T) {
	manager := NewTomlConfigManager()

	// Test case 1: Empty prefix
	t.Run("EmptyPrefix", func(t *testing.T) {
		path := manager.buildCurrentPath("", "key")
		if path != "key" {
			t.Errorf("Expected 'key', got '%s'", path)
		}
	})

	// Test case 2: With prefix
	t.Run("WithPrefix", func(t *testing.T) {
		path := manager.buildCurrentPath("database", "host")
		if path != "database.host" {
			t.Errorf("Expected 'database.host', got '%s'", path)
		}
	})
}

func Test_ValidateArray(t *testing.T) {
	manager := NewTomlConfigManager()

	// Test case 1: Valid array
	t.Run("ValidArray", func(t *testing.T) {
		actual := []interface{}{"a", "b", "c"}
		expected := []interface{}{"a", "b", "c"}

		if !manager.validateArray(actual, expected) {
			t.Error("Expected validation to pass for matching arrays")
		}
	})

	// Test case 2: Wrong type
	t.Run("WrongType", func(t *testing.T) {
		actual := "not_an_array"
		expected := []interface{}{"a", "b", "c"}

		if manager.validateArray(actual, expected) {
			t.Error("Expected validation to fail for wrong type")
		}
	})

	// Test case 3: Different lengths
	t.Run("DifferentLengths", func(t *testing.T) {
		actual := []interface{}{"a", "b"}
		expected := []interface{}{"a", "b", "c"}

		if manager.validateArray(actual, expected) {
			t.Error("Expected validation to fail for different lengths")
		}
	})

	// Test case 4: Different content
	t.Run("DifferentContent", func(t *testing.T) {
		actual := []interface{}{"a", "x", "c"}
		expected := []interface{}{"a", "b", "c"}

		if manager.validateArray(actual, expected) {
			t.Error("Expected validation to fail for different content")
		}
	})
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
