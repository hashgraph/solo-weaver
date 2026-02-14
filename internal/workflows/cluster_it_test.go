// SPDX-License-Identifier: Apache-2.0

// Integration tests for cluster setup and teardown.
//
// Build Tag: cluster_setup
//
// These tests are NOT part of the standard `integration` test suite.
// They are executed in a specific order by the Taskfile `test:integration:verbose` task:
//
//   Phase 1: go test -tags='cluster_setup' -run '^Test_ClusterSetup$' ./internal/workflows/...
//            → Creates the Kubernetes cluster and leaves it running
//
//   Phase 2: go test -tags='require_cluster' ./...
//            → Runs tests that require a running cluster (kube client tests, helm tests)
//
//   Phase 3: go test -tags='integration' ./...
//            → Runs general integration tests
//
//   Phase 4: go test -tags='cluster_setup' ./internal/workflows/...
//            → Runs Test_ClusterSetup (ensures cluster is up) then Test_ClusterTeardown (tears it down)
//
// Dependencies:
//   - Test_ClusterSetup: No dependencies, creates a fresh cluster
//   - Test_ClusterTeardown: Should run after all cluster-dependent tests complete
//
// Note: These tests modify system state (kubeadm, bind mounts, systemd services).
// They require root privileges and should only run in isolated VM environments.

//go:build cluster_setup

package workflows

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/testutil"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/stretchr/testify/require"
)

// Test_ClusterSetup creates the cluster and leaves it running for other tests.
// This test is run first via -tags='cluster_setup' before other cluster-dependent tests.
func Test_ClusterSetup(t *testing.T) {
	testutil.Reset(t)

	installWf, err := InstallClusterWorkflow(core.NodeTypeBlock, core.ProfileLocal).
		WithExecutionMode(automa.StopOnError).
		Build()
	require.NoError(t, err)

	report := installWf.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)

	steps.PrintWorkflowReport(report, "")
	require.Equal(t, automa.StatusSuccess, report.Status)
}

// Test_ClusterTeardown removes the cluster after all cluster-dependent tests have run.
// This test is run last via -tags='cluster_setup' -run 'Test_ClusterTeardown'.
func Test_ClusterTeardown(t *testing.T) {
	uninstallWf, err := UninstallClusterWorkflow().
		WithExecutionMode(automa.ContinueOnError).
		Build()
	require.NoError(t, err)

	report := uninstallWf.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)

	steps.PrintWorkflowReport(report, "")
	require.Equal(t, automa.StatusSuccess, report.Status)

	// Verify system is clean after uninstall
	t.Run("verify_cleanup", func(t *testing.T) {
		// Verify crio service is stopped
		testutil.AssertBashCommand(t, "systemctl is-active crio || true", "inactive\n")

		// Verify kubernetes directories are not mounted
		testutil.AssertBashCommandFails(t, "mountpoint -q /etc/kubernetes")
		testutil.AssertBashCommandFails(t, "mountpoint -q /var/lib/kubelet")
		testutil.AssertBashCommandFails(t, "mountpoint -q /var/run/cilium")

		// Verify systemd service files are removed
		testutil.AssertBashCommand(t, "[ ! -f /usr/lib/systemd/system/crio.service ] && echo 'removed' || echo 'exists'", "removed\n")
		testutil.AssertBashCommand(t, "[ ! -d /usr/lib/systemd/system/kubelet.service.d ] && echo 'removed' || echo 'exists'", "removed\n")
		testutil.AssertBashCommand(t, "[ ! -f /usr/lib/systemd/system/kubelet.service ] && echo 'removed' || echo 'exists'", "removed\n")

		// Verify configuration directories are removed
		testutil.AssertBashCommand(t, "[ ! -d /etc/containers ] && echo 'removed' || echo 'exists'", "removed\n")
		testutil.AssertBashCommand(t, "[ ! -d /etc/crio ] && echo 'removed' || echo 'exists'", "removed\n")

		// Verify .kube directories are removed
		testutil.AssertBashCommand(t, "[ ! -d /root/.kube ] && echo 'removed' || echo 'exists'", "removed\n")
		testutil.AssertBashCommand(t, "[ ! -d /home/weaver/.kube ] && echo 'removed' || echo 'exists'", "removed\n")

		// Verify weaver directory cleanup (downloads, bin, and logs should be preserved)
		testutil.AssertBashCommand(t, "[ -d /opt/solo/weaver/downloads ] && echo 'preserved' || echo 'missing'", "preserved\n")
		testutil.AssertBashCommand(t, "[ -d /opt/solo/weaver/bin ] && echo 'preserved' || echo 'missing'", "preserved\n")
		testutil.AssertBashCommand(t, "[ -d /opt/solo/weaver/logs ] && echo 'preserved' || echo 'missing'", "preserved\n")

		// Verify that only the downloads, bin, and logs folders exist under /opt/solo/weaver
		testutil.AssertBashCommand(t, "ls -1 /opt/solo/weaver | wc -l | tr -d ' '", "3\n")
		testutil.AssertBashCommand(t, "ls -1 /opt/solo/weaver | sort", "bin\ndownloads\nlogs\n")
	})
}
