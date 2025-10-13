package software

import (
	"os"
	"testing"
)

const (
	tmpFolder = "/opt/provisioner/tmp"
)

// setupTestEnvironment creates a clean test environment and registers cleanup
func setupTestEnvironment(t *testing.T) {
	t.Helper()

	// Clean up any existing test artifacts
	_ = os.RemoveAll(tmpFolder)

	// Register cleanup to run after test completes
	t.Cleanup(func() {
		_ = os.RemoveAll(tmpFolder)
	})
}
