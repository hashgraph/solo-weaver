//go:build mockintegration

package steps

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/require"
)

func runAndTestStep(t *testing.T, step automa.Builder) *automa.Report {
	s, err := step.Build()
	require.NoError(t, err)
	require.NotNil(t, s)

	ctx := context.Background()
	r := s.Execute(ctx)
	require.False(t, r.HasError())
	require.True(t, r.IsSuccess())
	return r
}

// Test_BashScriptBasedClusterSetupSteps_Integration runs all bash script based steps to verify they work as expected.
// Note: This test requires root privileges and will modify the system it runs on.
// Here each step is separately executed so that we can debug which step fails if any. However, in practice we would run
// these as a workflow
func Test_BashScriptBasedIndividualStepSequence_Integration(t *testing.T) {
	runAndTestStep(t, bashSteps.SetupSandboxFolders())

	runAndTestStep(t, bashSteps.UpdateOS())
	runAndTestStep(t, bashSteps.InstallGnupg2())
	runAndTestStep(t, bashSteps.InstallConntrack())
	runAndTestStep(t, bashSteps.InstallSoCat())
	runAndTestStep(t, bashSteps.InstallEBTables())
	runAndTestStep(t, bashSteps.InstallNFTables())
	runAndTestStep(t, bashSteps.EnableAndStartNFTables())
	runAndTestStep(t, bashSteps.AutoRemovePackages())

	// setup env for k8s
	runAndTestStep(t, bashSteps.DisableSwap())
	runAndTestStep(t, bashSteps.InstallKernelModules())
	runAndTestStep(t, bashSteps.ConfigureSysctlForKubernetes())
	runAndTestStep(t, bashSteps.SetupBindMounts())

	// install CLI tools
	runAndTestStep(t, bashSteps.DownloadKubectl())
	runAndTestStep(t, bashSteps.InstallKubectl())
	runAndTestStep(t, bashSteps.DownloadK9s())
	runAndTestStep(t, bashSteps.InstallK9s())
	runAndTestStep(t, bashSteps.DownloadHelm())
	runAndTestStep(t, bashSteps.InstallHelm()) // required for MetalLB setup

	// In our Go based implementation we don't need this
	runAndTestStep(t, bashSteps.DownloadDasel())
	runAndTestStep(t, bashSteps.InstallDasel())

	// CRI-O setup
	runAndTestStep(t, bashSteps.DownloadCrio())
	runAndTestStep(t, bashSteps.InstallCrio())
	runAndTestStep(t, bashSteps.ConfigureSandboxCrio())
	runAndTestStep(t, bashSteps.EnableAndStartCrio())

	// kubeadm setup
	runAndTestStep(t, bashSteps.DownloadKubeadm())
	runAndTestStep(t, bashSteps.InstallKubadm())
	runAndTestStep(t, bashSteps.TorchPriorKubeAdmConfiguration())
	runAndTestStep(t, bashSteps.DownloadKubeadmConfig())
	runAndTestStep(t, bashSteps.ConfigureKubeadm())

	// kubelet setup
	runAndTestStep(t, bashSteps.DownloadKubelet())
	runAndTestStep(t, bashSteps.InstallKubelet())
	runAndTestStep(t, bashSteps.DownloadKubeletConfig())
	runAndTestStep(t, bashSteps.ConfigureKubelet())
	runAndTestStep(t, bashSteps.EnableAndStartKubelet())

	// init cluster
	runAndTestStep(t, bashSteps.InitCluster())
	runAndTestStep(t, bashSteps.ConfigureKubeConfig())

	// Cilium CNI setup
	runAndTestStep(t, bashSteps.DownloadCiliumCli())
	runAndTestStep(t, bashSteps.InstallCiliumCli())
	runAndTestStep(t, bashSteps.ConfigureCiliumCNI())
	runAndTestStep(t, bashSteps.InstallCiliumCNI())
	runAndTestStep(t, bashSteps.EnableAndStartCiliumCNI())

	// MetalLB setup
	runAndTestStep(t, bashSteps.InstallMetalLB())
	runAndTestStep(t, bashSteps.ConfigureMetalLB())

	runAndTestStep(t, bashSteps.CheckClusterHealth())
}
