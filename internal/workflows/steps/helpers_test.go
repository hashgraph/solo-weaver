package steps

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/pkg/software"
)

func sudo(cmd *exec.Cmd) *exec.Cmd {
	if os.Geteuid() == 0 {
		return cmd
	}

	// Prepend sudo to the command
	sudoCmd := exec.Command("sudo", append([]string{cmd.Path}, cmd.Args[1:]...)...)
	sudoCmd.Stdout = cmd.Stdout
	sudoCmd.Stderr = cmd.Stderr
	sudoCmd.Stdin = cmd.Stdin

	return sudoCmd
}

func TestRunCmd_Success(t *testing.T) {
	out, err := runCmd("echo hello")
	require.NoError(t, err)
	require.Equal(t, "hello", out)
}

func TestRunCmd_Error(t *testing.T) {
	_, err := runCmd("exit 42")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to execute bash command")
}

func cleanUpTempDir(t *testing.T) {
	t.Helper()

	_ = exec.Command("chattr", "-Ri", core.Paths().TempDir).Run()

	_ = os.RemoveAll(core.Paths().TempDir)

	_ = os.RemoveAll(core.Paths().SandboxDir)

	// List files in /usr/local/bin and remove them
	files, err := os.ReadDir("/usr/local/bin")
	if err == nil {
		for _, file := range files {
			_ = os.Remove("/usr/local/bin/" + file.Name())
		}
	}
}

// reset performs a complete cleanup of the Kubernetes environment
func reset(t *testing.T) {
	t.Helper()

	// Reset kubeadm with custom CRI socket
	_ = sudo(exec.Command("kubeadm", "reset",
		"--cri-socket", "unix:///opt/provisioner/sandbox/var/run/crio/crio.sock",
		"--force")).Run()

	// Stop CRI-O service
	_ = sudo(exec.Command("systemctl", "stop", "crio")).Run()

	// Unmount kubernetes directories
	_ = sudo(exec.Command("umount", "/etc/kubernetes")).Run()
	_ = sudo(exec.Command("umount", "/var/lib/kubelet")).Run()
	_ = sudo(exec.Command("umount", "-R", "/var/run/cilium")).Run()

	// Remove provisioner directory
	_ = sudo(exec.Command("rm", "-rf", "/opt/provisioner")).Run()

	// Remove /usr/lib/systemd/system
	_ = sudo(exec.Command("rm", "-rf", "/usr/lib/systemd/system/crio.service")).Run()
	_ = sudo(exec.Command("rm", "-rf", "/usr/lib/systemd/system/kubelet.service.d")).Run()
	_ = sudo(exec.Command("rm", "-rf", "/usr/lib/systemd/system/kubelet.service")).Run()

	// Remove etc/containers directory
	_ = sudo(exec.Command("rm", "-rf", "/etc/containers")).Run()

	// Remove crio directory
	_ = sudo(exec.Command("rm", "-rf", "/etc/crio")).Run()

	// Clean up temp directory (from existing tests)
	cleanUpTempDir(t)
}

type SetupLevel int

const (
	SetupBasicLevel SetupLevel = iota
	SetupKubeletLevel
	SetupCrioLevel
	SetupCKubeadmLevel
)

// setupPrerequisitesToLevel sets up all the required components before cluster initialization
func setupPrerequisitesToLevel(t *testing.T, level SetupLevel) {
	t.Helper()

	// preflight & basic setup
	step, err := SetupHomeDirectoryStructure(core.Paths()).Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup home directory structure")

	step, err = RefreshSystemPackageIndex().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to refresh system package index")

	step, err = InstallSystemPackage("iptables", software.NewIptables).Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to install iptables")

	step, err = InstallSystemPackage("gpg", software.NewGpg).Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to install gpg")

	step, err = InstallSystemPackage("conntrack", software.NewConntrack).Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to install conntrack")

	step, err = InstallSystemPackage("ebtables", software.NewEbtables).Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to install ebtables")

	step, err = InstallSystemPackage("socat", software.NewSocat).Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to install socat")

	step, err = InstallSystemPackage("nftables", software.NewNftables).Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to install nftables")

	step, err = SetupSystemdService("nftables").Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup nftables service")

	step, err = InstallKernelModule("overlay").Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to install overlay kernel module")

	step, err = InstallKernelModule("br_netfilter").Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to install br_netfilter kernel module")

	step, err = AutoRemoveOrphanedPackages().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to auto-remove orphaned packages")

	// Disable swap
	step, err = DisableSwap().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to disable swap")

	// Configure sysctl for Kubernetes
	step, err = ConfigureSysctlForKubernetes().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to configure sysctl")

	// Setup bind mounts
	step, err = SetupBindMounts().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup bind mounts")

	// Setup kubectl
	step, err = SetupKubectl().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup kubectl")

	if level == SetupBasicLevel {
		return
	}

	// Setup kubelet
	step, err = SetupKubelet().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup kubelet")

	// Setup kubelet systemd service
	step, err = SetupSystemdService(software.KubeletServiceName).Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup kubelet service")

	if level == SetupKubeletLevel {
		return
	}

	// Setup CRI-O
	step, err = SetupCrio().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup CRI-O")

	// Setup CRI-O systemd service
	step, err = SetupSystemdService(software.CrioServiceName).Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup CRI-O service")

	if level == SetupCrioLevel {
		return
	}

	// Setup Kubeadm
	step, err = SetupKubeadm().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup Kubeadm")

	// Initialize cluster
	step, err = InitializeCluster().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to initialize cluster")

	if level == SetupCKubeadmLevel {
		return
	}
}
