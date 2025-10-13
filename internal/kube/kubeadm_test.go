package kube

import (
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestGenerateKubeadmToken(t *testing.T) {
	token, err := GenerateKubeadmToken()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Token should match the format: [a-z0-9]{6}\.[a-z0-9]{16}
	re := regexp.MustCompile(`^[a-z0-9]{6}\.[a-z0-9]{16}$`)
	if !re.MatchString(token) {
		t.Errorf("token %q does not match expected format", token)
	}
}

func TestReplaceKubeletPath(t *testing.T) {
	// Create a temp dir and file
	tmpDir := t.TempDir()
	origPath := filepath.Join(tmpDir, "10-kubeadm.conf")
	origContent := "ExecStart=/usr/bin/kubelet $KUBELET_KUBEADM_ARGS\n"
	if err := os.WriteFile(origPath, []byte(origContent), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	newKubeletPath := "/custom/bin/kubelet"
	if err := replaceKubeletPath(origPath, newKubeletPath); err != nil {
		t.Fatalf("replaceKubeletPath failed: %v", err)
	}

	// Read back and check
	updated, err := os.ReadFile(origPath)
	if err != nil {
		t.Fatalf("failed to read updated file: %v", err)
	}
	if !strings.Contains(string(updated), newKubeletPath) {
		t.Errorf("expected file to contain new kubelet path %q, got %q", newKubeletPath, string(updated))
	}
	if strings.Contains(string(updated), "/usr/bin/kubelet") {
		t.Errorf("old kubelet path still present in file")
	}
}

func TestCreateSymlink(t *testing.T) {
	tmpDir := t.TempDir()
	sourceDir := filepath.Join(tmpDir, "source")
	targetDir := filepath.Join(tmpDir, "target")

	// Create the source directory
	if err := os.Mkdir(sourceDir, 0755); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}

	// Create the symlink
	if err := createSymlink(sourceDir, targetDir); err != nil {
		t.Fatalf("createSymlink failed: %v", err)
	}

	// Check if the symlink exists and points to the sourceDir
	link, err := os.Readlink(targetDir)
	if err != nil {
		t.Fatalf("failed to read symlink: %v", err)
	}
	if link != sourceDir {
		t.Errorf("symlink points to %q, want %q", link, sourceDir)
	}
}

func TestGetMachineIP_Integration(t *testing.T) {
	ip, err := getMachineIP()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		t.Fatalf("returned IP %q is not a valid IP address", ip)
	}
	if parsed.IsLoopback() {
		t.Errorf("returned IP %q is a loopback address", ip)
	}
	if parsed.To4() == nil {
		t.Errorf("returned IP %q is not an IPv4 address", ip)
	}
}
