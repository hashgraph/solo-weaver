// SPDX-License-Identifier: Apache-2.0

//go:build integration

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

func Test_ClusterWorkflow_Integration(t *testing.T) {
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

	uninstallWf, err := UninstallClusterWorkflow().
		WithExecutionMode(automa.ContinueOnError).
		Build()
	require.NoError(t, err)

	report = uninstallWf.Execute(context.Background())
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
