// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// PrepareSubCmdForTest creates a root command with the given subcommand added.
// Use this from tests in other packages to avoid duplicating the helper.
func PrepareSubCmdForTest(sub *cobra.Command) *cobra.Command {
	root := &cobra.Command{Use: "root"}
	root.AddCommand(sub)
	return root
}

// Sudo prepends Sudo to the command if not running as root
func Sudo(cmd *exec.Cmd) *exec.Cmd {
	if os.Geteuid() == 0 {
		return cmd
	}

	// Prepend Sudo to the command
	sudoCmd := exec.Command("sudo", append([]string{cmd.Path}, cmd.Args[1:]...)...)
	sudoCmd.Stdout = cmd.Stdout
	sudoCmd.Stderr = cmd.Stderr
	sudoCmd.Stdin = cmd.Stdin

	return sudoCmd
}

// CleanupWeaverPreservingDownloads removes all weaver directories except the shared downloads folder
// This preserves downloaded files with valid checksums to speed up subsequent test runs
func CleanupWeaverPreservingDownloads() {
	weaverHome := "/opt/solo/weaver"
	downloadsFolder := core.Paths().DownloadsDir

	// Read the weaver home directory
	entries, err := os.ReadDir(weaverHome)
	if err != nil {
		// Directory doesn't exist, nothing to clean
		return
	}

	// Remove each top-level directory/file except downloads
	for _, entry := range entries {
		entryPath := filepath.Join(weaverHome, entry.Name())

		// Skip the downloads folder
		if entryPath == downloadsFolder {
			continue
		}

		// Remove all other directories/files
		_ = os.RemoveAll(entryPath)
	}
}

// CleanUpTempDir removes temporary files and directories created during tests
func CleanUpTempDir(t *testing.T) {
	t.Helper()

	_ = exec.Command("chattr", "-Ri", core.Paths().HomeDir).Run()

	_ = os.RemoveAll(core.Paths().TempDir)

	_ = os.RemoveAll(core.Paths().StateDir)

	_ = os.RemoveAll(core.Paths().SandboxDir)

	// List files in /usr/local/bin and remove them
	files, err := os.ReadDir("/usr/local/bin")
	if err == nil {
		for _, file := range files {
			_ = os.Remove("/usr/local/bin/" + file.Name())
		}
	}
}

// Reset performs a complete cleanup of the Kubernetes environment
func Reset(t *testing.T) {
	t.Helper()

	// Reset kubeadm with custom CRI socket
	_ = Sudo(exec.Command("kubeadm", "reset",
		"--cri-socket", "unix:///opt/solo/weaver/sandbox/var/run/crio/crio.sock",
		"--force")).Run()

	// Stop CRI-O service
	_ = Sudo(exec.Command("systemctl", "stop", "crio")).Run()

	// Unmount kubernetes directories
	_ = Sudo(exec.Command("umount", "/etc/kubernetes")).Run()
	_ = Sudo(exec.Command("umount", "/var/lib/kubelet")).Run()
	_ = Sudo(exec.Command("umount", "-R", "/var/run/cilium")).Run()

	// Remove weaver directory but preserve downloads folder for caching
	CleanupWeaverPreservingDownloads()

	// Remove /usr/lib/systemd/system
	_ = Sudo(exec.Command("rm", "-rf", "/usr/lib/systemd/system/crio.service")).Run()
	_ = Sudo(exec.Command("rm", "-rf", "/usr/lib/systemd/system/kubelet.service.d")).Run()
	_ = Sudo(exec.Command("rm", "-rf", "/usr/lib/systemd/system/kubelet.service")).Run()

	// Remove etc/containers directory
	_ = Sudo(exec.Command("rm", "-rf", "/etc/containers")).Run()

	// Remove crio directory
	_ = Sudo(exec.Command("rm", "-rf", "/etc/crio")).Run()

	// Remove .kube/config
	_ = Sudo(exec.Command("rm", "-rf", "/root/.kube")).Run()
	_ = Sudo(exec.Command("rm", "-rf", "/home/weaver/.kube")).Run()

	// Clean up temp directory (from existing tests)
	CleanUpTempDir(t)
}

func FileWithPrefixExists(t *testing.T, directory string, prefix string) bool {
	t.Helper()

	files, err := os.ReadDir(directory)
	require.NoError(t, err)
	found := false
	for _, f := range files {
		if strings.HasPrefix(f.Name(), prefix) {
			found = true
			break
		}
	}
	return found
}
