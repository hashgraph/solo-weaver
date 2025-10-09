//go:build integration

package steps

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/internal/templates"
	"golang.hedera.com/solo-provisioner/pkg/software"
)

func Test_ConfigureSysctlForKubernetes_Integration(t *testing.T) {
	setup, err := automa.NewWorkflowBuilder().WithId("test-sysctl").Steps(
		InstallKernelModule("br_netfilter"),
		InstallSystemPackage("nftables", software.NewNftables),
	).Build()
	require.NoError(t, err)
	report := setup.Execute(context.Background())
	require.NoError(t, report.Error)

	// cleanup
	defer func() {
		report = setup.Rollback(context.Background())
		assert.NoError(t, report.Error)

		_, err = templates.RemoveSysctlConfigurationFiles()
		assert.NoError(t, err)
		_, err = runCmd("sysctl --system")
		assert.NoError(t, err)
	}()

	// execute test
	wf, err := ConfigureSysctlForKubernetes().Build()
	require.NoError(t, err)
	report = wf.Execute(context.Background())
	require.NoError(t, report.Error)
	require.Equal(t, report.StepReports[0].Metadata["copied_files"],
		"/etc/sysctl.d/75-inotify.conf, /etc/sysctl.d/75-k8s-networking.conf, /etc/sysctl.d/75-network-performance.conf")
	require.Equal(t, automa.StatusSuccess, report.Status)
}
