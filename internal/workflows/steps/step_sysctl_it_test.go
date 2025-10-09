//go:build integration

package steps

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/require"
)

func Test_ConfigureSysctlForKubernetes_Integration(t *testing.T) {
	wf, err := ConfigureSysctlForKubernetes().Build()
	require.NoError(t, err)

	report := wf.Execute(context.Background())
	require.NoError(t, report.Error)
	require.Equal(t, report.StepReports[0].Metadata["copied_files"],
		"/etc/sysctl.d/75-inotify.conf, /etc/sysctl.d/75-k8s-networking.conf, /etc/sysctl.d/75-network-performance.conf")

	require.Equal(t, automa.StatusSuccess, report.Status)
}
