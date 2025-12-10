// SPDX-License-Identifier: Apache-2.0

//go:build integration

package steps

import (
	"context"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/sysctl"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	t.Cleanup(func() {
		// revert sysctl changes
		report := wf.Rollback(context.Background())
		assert.NoError(t, report.Error) // assert failure won't stop cleanup

		// revert setup
		report = setup.Rollback(context.Background())
		assert.NoError(t, report.Error) // assert failure won't stop cleanup

	})

	// setup prerequisites
	report := setup.Execute(context.Background())
	require.NoError(t, report.Error)

	// execute test
	require.NoError(t, err)
	report = wf.Execute(context.Background())
	require.NoError(t, report.Error)
	require.Equal(t, report.StepReports[0].Metadata["copied_files"],
		"/etc/sysctl.d/75-inotify.conf, /etc/sysctl.d/75-k8s-networking.conf, /etc/sysctl.d/75-network-performance.conf, /etc/sysctl.d/99-kubernetes-cri.conf")
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

	expectedTotalSettings := 31 // there are 31 settings in total

	oldSettings, err := sysctl.CurrentCandidateSettings()
	require.NoError(t, err)
	require.Equal(t, expectedTotalSettings, len(oldSettings))

	backupFile := path.Join(core.Paths().TempDir, "sysctl.conf")
	backupFile, err = sysctl.BackupSettings(backupFile)
	require.NoError(t, err)
	require.NotEmpty(t, backupFile)
	require.FileExists(t, backupFile)
	t.Cleanup(func() {
		// revert setup
		report = setup.Rollback(context.Background())
		assert.NoError(t, report.Error) // assert failure won't stop cleanup
		_ = os.RemoveAll(backupFile)
	})

	files, err := sysctl.CopyConfiguration()
	require.NoError(t, err)
	require.NotEmpty(t, files)

	applied, err := sysctl.LoadAllConfiguration()
	require.NoError(t, err)
	require.NotEmpty(t, applied)
	// Note: applied may include pre-existing system config files, so we check >= instead of ==
	require.GreaterOrEqual(t, len(applied), len(files), "applied configs should include at least all copied files")

	// check that all copied files are in the applied list
	for _, f := range files {
		require.Contains(t, applied, f)
	}

	newSettings, err := sysctl.CurrentCandidateSettings()
	require.NoError(t, err)
	require.Equal(t, expectedTotalSettings, len(newSettings))

	// Verify that current settings match desired settings from templates
	desiredSettings, err := sysctl.DesiredCandidateSettings()
	require.NoError(t, err)

	for k, desiredValue := range desiredSettings {
		require.Contains(t, newSettings, k, "setting %s should exist", k)
		// Normalize whitespace for comparison (sysctl returns tabs, templates may use spaces)
		actualNormalized := normalizeWhitespace(newSettings[k])
		desiredNormalized := normalizeWhitespace(desiredValue)
		require.Equal(t, desiredNormalized, actualNormalized, "setting %s should have desired value", k)
	}

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

// normalizeWhitespace normalizes whitespace in a string by replacing all sequences of whitespace
// (spaces, tabs, etc.) with a single space. This is useful for comparing sysctl values where
// the system may return tabs but templates use spaces.
func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
