package steps

import (
	"os"
	"path"
	"testing"
)

func Test_backupFstabFile(t *testing.T) {
	// Setup temp dirs and files
	tmpDir := t.TempDir()
	origFstab := path.Join(tmpDir, "fstab")
	backupDir := path.Join(tmpDir, "etc")
	backupFstab := path.Join(backupDir, "fstab")

	// Override global vars for test
	FstabPath = origFstab
	EtcBackupDir = backupDir

	// Write dummy fstab
	content := "UUID=swap-uuid none swap sw 0 0\n"
	if err := os.WriteFile(origFstab, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write fstab: %v", err)
	}

	// Create backup dir
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatalf("failed to create backup dir: %v", err)
	}

	// Run step
	step, err := backupFstabFile().Build()
	if err != nil {
		t.Fatalf("failed to build step: %v", err)
	}
	_, err = step.Execute(nil)
	if err != nil {
		t.Fatalf("step execution failed: %v", err)
	}

	// Check backup exists and content matches
	data, err := os.ReadFile(backupFstab)
	if err != nil {
		t.Fatalf("backup file not found: %v", err)
	}
	if string(data) != content {
		t.Errorf("backup content mismatch: got %q, want %q", string(data), content)
	}
}
