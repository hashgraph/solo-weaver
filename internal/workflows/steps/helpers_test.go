// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/stretchr/testify/require"
)

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

type SetupLevel int

const (
	SetupBasicLevel SetupLevel = iota
	SetupKubeletLevel
	SetupCrioLevel
	SetupKubeadmLevel
	SetupCiliumLevel
	SetupMetalLBLevel
)

// SetupPrerequisitesToLevel sets up all the required components before cluster initialization
func SetupPrerequisitesToLevel(t *testing.T, level SetupLevel) {
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

	if level == SetupKubeadmLevel {
		return
	}

	// Setup Cilium
	step, err = SetupCilium().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup Cilium")

	// Start Cilium
	step, err = StartCilium().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to start Cilium")

	if level == SetupCiliumLevel {
		return
	}

	// Setup MetalLB
	step, err = SetupMetalLB().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup MetalLB")

	if level == SetupMetalLBLevel {
		return
	}
}
