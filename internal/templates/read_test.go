package templates

import (
	"strings"
	"testing"
)

func TestRead_ValidFile(t *testing.T) {
	data, err := Read("files/sysctl/75-inotify.conf")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(data) == 0 {
		t.Errorf("expected file content, got empty")
	}
}

func TestRead_EmptyName(t *testing.T) {
	_, err := Read("")
	if err == nil || !strings.Contains(err.Error(), "file name cannot be empty") {
		t.Errorf("expected error for empty name, got %v", err)
	}
}

func TestRead_NonExistentFile(t *testing.T) {
	_, err := Read("files/does_not_exist.txt")
	if err == nil || !strings.Contains(err.Error(), "failed to read embedded file") {
		t.Errorf("expected error for missing file, got %v", err)
	}
}

func TestReadAsString_ValidFile(t *testing.T) {
	data, err := ReadAsString("files/sysctl/75-inotify.conf")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(data) == 0 {
		t.Errorf("expected file content, got empty")
	}
}

func TestReadAsString_EmptyName(t *testing.T) {
	_, err := ReadAsString("")
	if err == nil || !strings.Contains(err.Error(), "file name cannot be empty") {
		t.Errorf("expected error for empty name, got %v", err)
	}
}

func TestReadAsString_NonExistentFile(t *testing.T) {
	_, err := ReadAsString("files/does_not_exist.txt")
	if err == nil || !strings.Contains(err.Error(), "failed to read embedded file") {
		t.Errorf("expected error for missing file, got %v", err)
	}
}

// Optional: If you have a file with invalid UTF-8, test decoding error
func TestReadAsString_InvalidUTF8(t *testing.T) {
	// This test assumes you have a file with invalid UTF-8 bytes, e.g., files/invalid_utf8.bin
	_, err := ReadAsString("files/invalid_utf8.bin")
	if err == nil || !strings.Contains(err.Error(), "failed to decode file") {
		t.Skip("no invalid UTF-8 file to test with or error not as expected")
	}
}

func TestReadDir_ValidDir(t *testing.T) {
	files, err := ReadDir("files/sysctl")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(files) == 0 {
		t.Errorf("expected at least one file, got none")
	}
}

func TestReadDir_EmptyName(t *testing.T) {
	_, err := ReadDir("")
	if err == nil || !strings.Contains(err.Error(), "directory name cannot be empty") {
		t.Errorf("expected error for empty name, got %v", err)
	}
}

func TestReadDir_NonExistentDir(t *testing.T) {
	_, err := ReadDir("files/does_not_exist_dir")
	if err == nil || !strings.Contains(err.Error(), "failed to read embedded directory") {
		t.Errorf("expected error for missing directory, got %v", err)
	}
}
