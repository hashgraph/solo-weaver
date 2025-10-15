package software

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
)

func Test_BaseInstaller_replaceAllInFile(t *testing.T) {
	fsxManager, err := fsx.NewManager()
	require.NoError(t, err)

	ki := kubeadmInstaller{
		baseInstaller: &baseInstaller{
			fileManager: fsxManager,
		},
	}

	// Create a temp dir and file
	tmpDir := t.TempDir()
	origPath := filepath.Join(tmpDir, "10-kubeadm.conf")
	origContent := "ExecStart=/usr/bin/kubelet $KUBELET_KUBEADM_ARGS\n"
	if err := os.WriteFile(origPath, []byte(origContent), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	newKubeletPath := "/custom/bin/kubelet"
	if err := ki.replaceAllInFile(origPath, "/usr/bin/kubelet", newKubeletPath); err != nil {
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
