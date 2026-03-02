// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/testutil"
	"github.com/hashgraph/solo-weaver/pkg/models"
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

// prepareStateManager creates a state.Manager for use in unit tests.
func prepareStateManager(t *testing.T) state.Manager {
	t.Helper()
	sm, err := state.NewStateManager()
	require.NoError(t, err, "failed to create state manager for test")
	return sm
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
	sm := prepareStateManager(t)

	// preflight & basic setup
	step, err := SetupKubectl(sm).Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup kubectl")

	if level == SetupBasicLevel {
		return
	}

	// Setup kubelet
	step, err = SetupKubelet(sm).Build()
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
	step, err = SetupCrio(sm).Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup CRI-O")

	// Sets up registry proxy for CRI-O
	err = testutil.InstallCrioRegistriesConf()
	require.NoError(t, err)

	// Setup CRI-O systemd service
	step, err = SetupSystemdService(software.CrioServiceName).Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup CRI-O service")

	if level == SetupCrioLevel {
		return
	}

	// Setup Kubeadm
	step, err = SetupKubeadm(sm).Build()
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
	step, err = SetupCilium(sm).Build()
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

	_ = models.Paths() // ensure models is used
}
