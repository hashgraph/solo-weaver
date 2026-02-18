// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

	t.Log("Reset: Starting cleanup of Kubernetes environment")

	// Try to reset kubeadm - use sandbox kubeadm if available, otherwise use system kubeadm
	sandboxKubeadm := "/opt/solo/weaver/sandbox/bin/kubeadm"
	kubeadmBin := "kubeadm"
	if _, err := os.Stat(sandboxKubeadm); err == nil {
		kubeadmBin = sandboxKubeadm
	}

	// Reset kubeadm with custom CRI socket
	t.Logf("Reset: Running kubeadm reset using %s", kubeadmBin)
	if out, err := Sudo(exec.Command(kubeadmBin, "reset",
		"--cri-socket", "unix:///opt/solo/weaver/sandbox/var/run/crio/crio.sock",
		"--force")).CombinedOutput(); err != nil {
		t.Logf("Reset: kubeadm reset failed (may be expected): %v, output: %s", err, string(out))
	}

	// Stop kubelet service
	t.Log("Reset: Stopping kubelet service")
	_ = Sudo(exec.Command("systemctl", "stop", "kubelet")).Run()

	// Stop CRI-O service
	t.Log("Reset: Stopping crio service")
	_ = Sudo(exec.Command("systemctl", "stop", "crio")).Run()

	// Kill kubernetes control plane processes directly
	t.Log("Reset: Killing kubernetes control plane processes")
	_ = Sudo(exec.Command("killall", "-9", "kube-apiserver")).Run()
	_ = Sudo(exec.Command("killall", "-9", "kube-controller-manager")).Run()
	_ = Sudo(exec.Command("killall", "-9", "kube-scheduler")).Run()
	_ = Sudo(exec.Command("killall", "-9", "etcd")).Run()
	_ = Sudo(exec.Command("killall", "-9", "kubelet")).Run()

	// Kill any processes using kubernetes ports
	t.Log("Reset: Killing processes on kubernetes ports")
	_ = Sudo(exec.Command("bash", "-c", "fuser -k 6443/tcp 2>/dev/null || true")).Run()
	_ = Sudo(exec.Command("bash", "-c", "fuser -k 10250/tcp 2>/dev/null || true")).Run()
	_ = Sudo(exec.Command("bash", "-c", "fuser -k 10259/tcp 2>/dev/null || true")).Run()
	_ = Sudo(exec.Command("bash", "-c", "fuser -k 10257/tcp 2>/dev/null || true")).Run()
	_ = Sudo(exec.Command("bash", "-c", "fuser -k 2379/tcp 2>/dev/null || true")).Run()
	_ = Sudo(exec.Command("bash", "-c", "fuser -k 2380/tcp 2>/dev/null || true")).Run()

	// Small delay to allow ports to be released
	t.Log("Reset: Waiting 2s for ports to be released")
	time.Sleep(2 * time.Second)

	// Check if ports are actually free
	if out, _ := exec.Command("bash", "-c", "ss -tlnp | grep -E ':(6443|10250|2379|2380) ' || echo 'ports are free'").CombinedOutput(); len(out) > 0 {
		t.Logf("Reset: Port status after cleanup: %s", strings.TrimSpace(string(out)))
	}

	// Unmount kubernetes directories (lazy unmount)
	t.Log("Reset: Unmounting kubernetes directories")
	_ = Sudo(exec.Command("umount", "-l", "/etc/kubernetes")).Run()
	_ = Sudo(exec.Command("umount", "-l", "/var/lib/kubelet")).Run()
	_ = Sudo(exec.Command("umount", "-lR", "/var/run/cilium")).Run()

	// Clean up /etc/kubernetes directory (in case unmount failed or it's not a bind mount)
	t.Log("Reset: Removing /etc/kubernetes")
	_ = Sudo(exec.Command("rm", "-rf", "/etc/kubernetes")).Run()

	// Clean up /var/lib/etcd (kubeadm stores etcd data here)
	t.Log("Reset: Removing /var/lib/etcd")
	_ = Sudo(exec.Command("rm", "-rf", "/var/lib/etcd")).Run()

	// Clean up /var/lib/kubelet
	t.Log("Reset: Removing /var/lib/kubelet")
	_ = Sudo(exec.Command("rm", "-rf", "/var/lib/kubelet")).Run()

	// Clean up CNI configuration
	t.Log("Reset: Removing CNI configuration")
	_ = Sudo(exec.Command("rm", "-rf", "/etc/cni/net.d")).Run()
	_ = Sudo(exec.Command("rm", "-rf", "/var/lib/cni")).Run()

	// Remove weaver directory but preserve downloads folder for caching
	t.Log("Reset: Cleaning weaver directories (preserving downloads)")
	CleanupWeaverPreservingDownloads()

	// Remove /usr/lib/systemd/system
	t.Log("Reset: Removing systemd unit files")
	_ = Sudo(exec.Command("rm", "-rf", "/usr/lib/systemd/system/crio.service")).Run()
	_ = Sudo(exec.Command("rm", "-rf", "/usr/lib/systemd/system/kubelet.service.d")).Run()
	_ = Sudo(exec.Command("rm", "-rf", "/usr/lib/systemd/system/kubelet.service")).Run()

	// Remove etc/containers directory
	_ = Sudo(exec.Command("rm", "-rf", "/etc/containers")).Run()

	// Remove crio directory
	_ = Sudo(exec.Command("rm", "-rf", "/etc/crio")).Run()

	// Remove .kube/config
	t.Log("Reset: Removing .kube directories")
	_ = Sudo(exec.Command("rm", "-rf", "/root/.kube")).Run()
	_ = Sudo(exec.Command("rm", "-rf", "/home/weaver/.kube")).Run()

	// Reload systemd to pick up removed unit files
	t.Log("Reset: Reloading systemd")
	_ = Sudo(exec.Command("systemctl", "daemon-reload")).Run()

	// Clean up temp directory (from existing tests)
	t.Log("Reset: Cleaning up temp directory")
	CleanUpTempDir(t)

	// Final verification
	if _, err := os.Stat("/etc/kubernetes"); err == nil {
		t.Log("Reset: WARNING - /etc/kubernetes still exists after cleanup")
	}
	if _, err := os.Stat("/var/lib/etcd"); err == nil {
		t.Log("Reset: WARNING - /var/lib/etcd still exists after cleanup")
	}

	t.Log("Reset: Cleanup complete")
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

// AssertBashCommand executes a bash command and asserts that it produces the expected output
func AssertBashCommand(t *testing.T, command string, expectedOutput string) {
	t.Helper()

	cmd := exec.Command("bash", "-c", command)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Command failed: %s\nOutput: %s", command, string(output))
	require.Equal(t, expectedOutput, string(output), "Command output mismatch for: %s", command)
}

// AssertBashCommandFails executes a bash command and asserts that it fails (non-zero exit code)
func AssertBashCommandFails(t *testing.T, command string) {
	t.Helper()

	cmd := exec.Command("bash", "-c", command)
	err := cmd.Run()
	require.Error(t, err, "Expected command to fail but it succeeded: %s", command)
}
