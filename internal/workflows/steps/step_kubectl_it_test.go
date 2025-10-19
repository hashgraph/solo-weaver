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

func Test_StepKubectl_Fresh_Integration(t *testing.T) {
	//
	// Given
	//
	cleanUpTempDir(t)

	//
	// When
	//
	step, err := SetupKubectl().Build()

	//
	// Then
	//
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)
}

func Test_StepKubectl_AlreadyInstalled_Integration(t *testing.T) {
	//
	// Given
	//
	cleanUpTempDir(t)

	step, err := SetupKubectl().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	//
	// When
	//
	step, err = SetupKubectl().Build()

	//
	// Then
	//
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

}

func Test_StepKubectl_PartiallyInstalled_Integration(t *testing.T) {
	//
	// Given
	//
	cleanUpTempDir(t)

	step, err := SetupKubectl().Build()
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
	step, err = SetupKubectl().Build()

	//
	// Then
	//
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)
}
