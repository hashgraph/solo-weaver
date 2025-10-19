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

func Test_StepK9s_Fresh_Integration(t *testing.T) {
	//
	// Given
	//
	cleanUpTempDir(t)

	//
	// When
	//
	step, err := SetupK9s().Build()

	//
	// Then
	//
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)
}

func Test_StepK9s_AlreadyInstalled_Integration(t *testing.T) {
	//
	// Given
	//
	cleanUpTempDir(t)

	step, err := SetupK9s().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	//
	// When
	//
	step, err = SetupK9s().Build()

	//
	// Then
	//
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

}

func Test_StepK9s_PartiallyInstalled_Integration(t *testing.T) {
	//
	// Given
	//
	cleanUpTempDir(t)

	step, err := SetupK9s().Build()
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
	step, err = SetupK9s().Build()

	//
	// Then
	//
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)
}
