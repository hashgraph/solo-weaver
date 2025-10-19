//go:build integration

package steps

import (
	"context"
	"os"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/internal/core"
)

func Test_StepKubeadm_Fresh_Integration(t *testing.T) {
	//
	// Given
	//
	cleanUpTempDir(t)

	//
	// When
	//
	step, err := SetupKubeadm().Build()

	//
	// Then
	//
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)
}

func Test_StepKubeadm_AlreadyInstalled_Integration(t *testing.T) {
	//
	// Given
	//
	cleanUpTempDir(t)

	step, err := SetupKubeadm().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	//
	// When
	//
	step, err = SetupKubeadm().Build()

	//
	// Then
	//
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

}

func Test_StepKubeadm_PartiallyInstalled_Integration(t *testing.T) {
	//
	// Given
	//
	cleanUpTempDir(t)

	step, err := SetupKubeadm().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	err = os.RemoveAll(core.Paths().TempDir)
	require.NoError(t, err)

	//
	// When
	//
	step, err = SetupKubeadm().Build()

	//
	// Then
	//
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)
}
