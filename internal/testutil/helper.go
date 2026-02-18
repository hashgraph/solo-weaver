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

// Bind mount targets used in tests
var bindMountTargets = []string{
	"/var/run/cilium",
	"/var/lib/kubelet",
	"/etc/kubernetes",
}

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

	// Stop CRI-O and docker services
	t.Log("Reset: Stopping CRI-O and docker services")
	for _, service := range []string{"crio", "docker"} {
		_ = Sudo(exec.Command("systemctl", "stop", service)).Run()
	}

	t.Log("Reset: Stopping crio service")
	_ = Sudo(exec.Command("systemctl", "stop", "crio")).Run()

	// Kill kubernetes control plane processes
	t.Log("Reset: Killing kubernetes control plane processes")
	for _, proc := range []string{"kube-apiserver", "kube-controller-manager", "kube-scheduler", "etcd", "kubelet"} {
		_ = Sudo(exec.Command("pkill", "-9", "-f", proc)).Run()
	}

	// Kill any processes using kubernetes ports
	t.Log("Reset: Killing processes on kubernetes ports")
	for _, port := range []string{"6443", "10250", "10259", "10257", "2379", "2380"} {
		_ = Sudo(exec.Command("bash", "-c", "fuser -k -9 "+port+"/tcp 2>/dev/null || true")).Run()
	}

	// Wait for ports to be released
	t.Log("Reset: Waiting for ports to be released")
	waitForPortsToBeReleased(t)

	// Unmount and cleanup bind mounts
	t.Log("Reset: Unmounting kubernetes directories")
	cleanupBindMounts(t)

	// Clean up directories
	t.Log("Reset: Removing /etc/kubernetes")
	_ = Sudo(exec.Command("rm", "-rf", "/etc/kubernetes")).Run()

	t.Log("Reset: Removing /var/lib/etcd")
	_ = Sudo(exec.Command("rm", "-rf", "/var/lib/etcd")).Run()

	t.Log("Reset: Removing /var/lib/kubelet")
	_ = Sudo(exec.Command("rm", "-rf", "/var/lib/kubelet")).Run()

	// Clean up /var/run/cilium and /run/cilium (symlink case)
	t.Log("Reset: Removing /var/run/cilium")
	_ = Sudo(exec.Command("rm", "-rf", "/var/run/cilium")).Run()
	_ = Sudo(exec.Command("rm", "-rf", "/run/cilium")).Run()

	// Clean up CNI configuration
	t.Log("Reset: Removing CNI configuration")
	_ = Sudo(exec.Command("rm", "-rf", "/etc/cni/net.d")).Run()
	_ = Sudo(exec.Command("rm", "-rf", "/var/lib/cni")).Run()

	// Remove weaver directory but preserve downloads folder for caching
	t.Log("Reset: Cleaning weaver directories (preserving downloads)")
	CleanupWeaverPreservingDownloads()

	// Remove systemd unit files
	t.Log("Reset: Removing systemd unit files")
	_ = Sudo(exec.Command("rm", "-rf", "/usr/lib/systemd/system/crio.service")).Run()
	_ = Sudo(exec.Command("rm", "-rf", "/usr/lib/systemd/system/kubelet.service.d")).Run()
	_ = Sudo(exec.Command("rm", "-rf", "/usr/lib/systemd/system/kubelet.service")).Run()

	// Remove etc/containers and crio directories
	_ = Sudo(exec.Command("rm", "-rf", "/etc/containers")).Run()
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

	t.Log("Reset: Cleanup complete")
}

// waitForPortsToBeReleased waits until kubernetes ports are free
func waitForPortsToBeReleased(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		out, _ := exec.Command("bash", "-c", "ss -tlnp 2>/dev/null | grep -E ':(6443|10250|2379|2380) ' || echo 'free'").CombinedOutput()
		if strings.TrimSpace(string(out)) == "free" {
			t.Log("Reset: All kubernetes ports are free")
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	if out, _ := exec.Command("bash", "-c", "ss -tlnp | grep -E ':(6443|10250|2379|2380) ' || echo 'ports are free'").CombinedOutput(); len(out) > 0 {
		t.Logf("Reset: Port status after cleanup: %s", strings.TrimSpace(string(out)))
	}
}

// cleanupBindMounts unmounts bind mounts and removes fstab entries
func cleanupBindMounts(t *testing.T) {
	t.Helper()

	for _, target := range bindMountTargets {
		// Unmount using lazy unmount (works even if busy)
		if isMountPoint(target) {
			t.Logf("Reset: Unmounting %s", target)
			_ = Sudo(exec.Command("umount", "-l", target)).Run()
		}

		// Also try resolved path for symlinks (e.g., /var/run -> /run)
		if resolved, err := filepath.EvalSymlinks(filepath.Dir(target)); err == nil {
			resolvedTarget := filepath.Join(resolved, filepath.Base(target))
			if resolvedTarget != target && isMountPoint(resolvedTarget) {
				t.Logf("Reset: Unmounting %s (resolved)", resolvedTarget)
				_ = Sudo(exec.Command("umount", "-l", resolvedTarget)).Run()
			}
		}

		// Remove fstab entry
		removeFstabEntry(t, target)
	}
}

// isMountPoint checks if a path is currently a mount point
func isMountPoint(path string) bool {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false
	}

	cleanPath := filepath.Clean(path)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == cleanPath {
			return true
		}
	}
	return false
}

// removeFstabEntry removes an fstab entry for the given target mount point
func removeFstabEntry(t *testing.T, target string) {
	t.Helper()

	data, err := os.ReadFile("/etc/fstab")
	if err != nil {
		return
	}

	var newLines []string
	found := false
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == target {
			found = true
			t.Logf("Reset: Removing fstab entry for %s", target)
			continue
		}
		newLines = append(newLines, line)
	}

	if found {
		content := strings.TrimRight(strings.Join(newLines, "\n"), "\n") + "\n"
		_ = os.WriteFile("/etc/fstab", []byte(content), 0644)
	}
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

// WriteTempManifest creates a temporary file with the given content and returns
// the file path and a cleanup function. This helper is shared across unit and
// integration tests.
func WriteTempManifest(t *testing.T, content string) (string, func()) {
	t.Helper()
	f, err := os.CreateTemp("", "parse-manifests-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		t.Fatalf("write temp file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	return f.Name(), cleanup
}
