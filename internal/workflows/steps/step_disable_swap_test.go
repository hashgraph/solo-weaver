package steps

import (
	"github.com/joomcode/errorx"
	"os"
	"path"
	"testing"
)

func Test_backupFstabFile(t *testing.T) {
	// Setup temp dirs and files
	tmpDir := t.TempDir()

	// Override global vars for test
	etcBackupDir = path.Join(tmpDir, "etc-backup")
	fstabBackupPath = path.Join(etcBackupDir, "fstab")
	etcDir = path.Join(tmpDir, "etc-restore")
	fstabPath = path.Join(etcDir, "fstab")

	// Create backup dir
	if err := os.MkdirAll(etcBackupDir, 0755); err != nil {
		t.Fatalf("failed to create backup dir: %v", err)
	}
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatalf("failed to create backup dir: %v", err)
	}

	// Write dummy fstab
	content := "UUID=swap-uuid none swap sw 0 0\n"
	if err := os.WriteFile(fstabPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write fstab: %v", err)
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
	data, err := os.ReadFile(fstabBackupPath)
	if err != nil {
		t.Fatalf("backup file not found: %v", err)
	}
	if string(data) != content {
		t.Errorf("backup content mismatch: got %q, want %q", string(data), content)
	}
}

func Test_restoreFstabFile(t *testing.T) {
	// Setup temp dirs and files
	tmpDir := t.TempDir()

	// Override global vars for test
	etcBackupDir = path.Join(tmpDir, "etc-backup")
	fstabBackupPath = path.Join(etcBackupDir, "fstab")
	etcDir = path.Join(tmpDir, "etc-restore")
	fstabPath = path.Join(etcDir, "fstab")

	// Create backup dir
	if err := os.MkdirAll(etcBackupDir, 0755); err != nil {
		t.Fatalf("failed to create backup dir: %v", err)
	}
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatalf("failed to create backup dir: %v", err)
	}

	// Write dummy backup fstab
	content := "UUID=swap-uuid none swap sw 0 0\n"
	if err := os.WriteFile(fstabBackupPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write backup fstab: %v", err)
	}

	// Run step
	step, err := restoreFstabFile().Build()
	if err != nil {
		t.Fatalf("failed to build step: %v", err)
	}
	_, err = step.Execute(nil)
	if err != nil {
		t.Fatalf("step execution failed: %v", err)
	}

	// Check restored file exists and content matches
	data, err := os.ReadFile(fstabPath)
	if err != nil {
		t.Fatalf("restored file not found: %v", err)
	}
	if string(data) != content {
		t.Errorf("restored content mismatch: got %q, want %q", string(data), content)
	}
}

func Test_commentSwapSettings(t *testing.T) {
	// Setup temp dirs and files
	tmpDir := t.TempDir()
	etcDir = path.Join(tmpDir, "etc")
	fstabPath = path.Join(etcDir, "fstab")

	// Create etc dir
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatalf("failed to create etc dir: %v", err)
	}

	// Write dummy fstab with swap entry, already commented swap, and normal entry
	content := "UUID=swap-uuid none swap sw 0 0\n#UUID=swap-uuid2 none swap sw 0 0\nUUID=root-uuid / ext4 defaults 0 1\n"
	if err := os.WriteFile(fstabPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write fstab: %v", err)
	}

	// Run step
	step, err := commentSwapSettings().Build()
	if err != nil {
		t.Fatalf("failed to build step: %v", err)
	}
	_, err = step.Execute(nil)
	if err != nil {
		t.Fatalf("step execution failed: %v", err)
	}

	// Check swap line is commented, already commented line unchanged, normal entry unchanged
	data, err := os.ReadFile(fstabPath)
	if err != nil {
		t.Fatalf("failed to read fstab: %v", err)
	}
	expected := "#UUID=swap-uuid none swap sw 0 0\n#UUID=swap-uuid2 none swap sw 0 0\nUUID=root-uuid / ext4 defaults 0 1\n"
	if string(data) != expected {
		t.Errorf("commented content mismatch: got %q, want %q", string(data), expected)
	}
}

func Test_swapOff_success(t *testing.T) {
	origRunCmd := RunCmd
	defer func() { RunCmd = origRunCmd }()
	RunCmd = func(name string, args ...string) error {
		if name != "swapoff" || len(args) != 1 || args[0] != "-a" {
			t.Errorf("unexpected command: %s %v", name, args)
		}
		return nil
	}

	step, err := swapOff().Build()
	if err != nil {
		t.Fatalf("failed to build step: %v", err)
	}
	_, err = step.Execute(nil)
	if err != nil {
		t.Errorf("expected success, got error: %v", err)
	}
}

func Test_swapOff_error(t *testing.T) {
	origRunCmd := RunCmd
	defer func() { RunCmd = origRunCmd }()
	RunCmd = func(name string, args ...string) error {
		return errorx.IllegalState.New("mock swapoff error")
	}

	step, err := swapOff().Build()
	if err != nil {
		t.Fatalf("failed to build step: %v", err)
	}
	_, err = step.Execute(nil)
	if err == nil {
		t.Errorf("expected error, got success")
	}
}

func Test_swapOn_success(t *testing.T) {
	origRunCmd := RunCmd
	defer func() { RunCmd = origRunCmd }()
	RunCmd = func(name string, args ...string) error {
		if name != "swapon" || len(args) != 1 || args[0] != "-a" {
			t.Errorf("unexpected command: %s %v", name, args)
		}
		return nil
	}

	step, err := swapOn().Build()
	if err != nil {
		t.Fatalf("failed to build step: %v", err)
	}
	_, err = step.Execute(nil)
	if err != nil {
		t.Errorf("expected success, got error: %v", err)
	}
}

func Test_swapOn_error(t *testing.T) {
	origRunCmd := RunCmd
	defer func() { RunCmd = origRunCmd }()
	RunCmd = func(name string, args ...string) error {
		return errorx.IllegalState.New("mock swapon error")
	}

	step, err := swapOn().Build()
	if err != nil {
		t.Fatalf("failed to build step: %v", err)
	}
	_, err = step.Execute(nil)
	if err == nil {
		t.Errorf("expected error, got success")
	}
}
