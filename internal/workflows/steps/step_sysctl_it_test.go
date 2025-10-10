//go:build integration

package steps

import (
	"context"
	"os"
	"path"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/internal/sysctl"
	"golang.hedera.com/solo-provisioner/pkg/software"
)

func Test_ConfigureSysctlForKubernetes_Integration(t *testing.T) {
	// instantiate workflows
	setup, err := automa.NewWorkflowBuilder().WithId("test-sysctl").Steps(
		InstallKernelModule("br_netfilter"),
		InstallSystemPackage("nftables", software.NewNftables),
	).Build()
	require.NoError(t, err)

	wf, err := ConfigureSysctlForKubernetes().Build()
	require.NoError(t, err)

	// cleanup
	defer func() {
		// revert setup
		report := setup.Rollback(context.Background())
		assert.NoError(t, report.Error) // assert failure won't stop cleanup

		// revert sysctl changes
		report = wf.Rollback(context.Background())
		assert.NoError(t, report.Error) // assert failure won't stop cleanup
	}()

	// setup prerequisites
	report := setup.Execute(context.Background())
	require.NoError(t, report.Error)

	// execute test
	require.NoError(t, err)
	report = wf.Execute(context.Background())
	require.NoError(t, report.Error)
	require.Equal(t, report.StepReports[0].Metadata["copied_files"],
		"/etc/sysctl.d/75-inotify.conf, /etc/sysctl.d/75-k8s-networking.conf, /etc/sysctl.d/75-network-performance.conf")
	require.Equal(t, automa.StatusSuccess, report.Status)
}

func Test_ApplySysctlConfiguration_Integration(t *testing.T) {
	// instantiate workflows
	setup, err := automa.NewWorkflowBuilder().WithId("test-sysctl").Steps(
		InstallKernelModule("br_netfilter"),
		InstallSystemPackage("nftables", software.NewNftables),
	).Build()
	require.NoError(t, err)
	report := setup.Execute(context.Background())
	require.NoError(t, report.Error)
	defer func() {
		// revert setup
		report = setup.Rollback(context.Background())
		assert.NoError(t, report.Error) // assert failure won't stop cleanup
	}()

	expectedTotalSettings := 31 // there are 31 settings in total

	oldSettings, err := sysctl.CurrentCandidateSettings()
	require.NoError(t, err)
	require.Equal(t, expectedTotalSettings, len(oldSettings))

	backupFile := path.Join(core.Paths().TempDir, "sysctl.conf")
	backupFile, err = sysctl.BackupSettings(backupFile)
	require.NoError(t, err)
	require.NotEmpty(t, backupFile)
	require.FileExists(t, backupFile)
	defer func() {
		_ = os.RemoveAll(backupFile)
	}()

	files, err := sysctl.CopyConfiguration()
	require.NoError(t, err)
	require.NotEmpty(t, files)

	applied, err := sysctl.LoadAllConfiguration()
	require.NoError(t, err)
	require.NotEmpty(t, applied)
	require.Equal(t, len(files), len(applied))

	// check that files and applied contain the same files
	for _, f := range files {
		require.Contains(t, applied, f)
	}

	newSettings, err := sysctl.CurrentCandidateSettings()
	require.NoError(t, err)
	require.Equal(t, expectedTotalSettings, len(newSettings))

	foundNotEqual := false
	for k, v := range oldSettings {
		require.Contains(t, newSettings, k)
		if v != newSettings[k] {
			foundNotEqual = true
		}
	}
	require.True(t, foundNotEqual) // at least one setting must have changed

	err = sysctl.RestoreSettings(backupFile)
	assert.NoError(t, err)

	restoredSettings, err := sysctl.CurrentCandidateSettings()
	require.NoError(t, err)
	require.Equal(t, expectedTotalSettings, len(restoredSettings)) // there are 31 settings in total

	// check old values are restored
	for k, v := range oldSettings {
		require.Contains(t, restoredSettings, k)
		require.Equal(t, v, restoredSettings[k])
	}
}
