package steps

import (
	"context"
	"os"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/internal/core"
)

func cleanUpCrio(t *testing.T) {
	t.Helper()

	err := os.RemoveAll(core.Paths().TempDir)
	require.NoError(t, err)
}

func TestSetupCrio_Fresh_Integration(t *testing.T) {
	//
	// Given
	//
	cleanUpCrio(t)

	//
	// When
	//
	step, err := SetupCrio().Build()

	//
	// Then
	//
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)
}

func TestSetupCrio_AlreadyInstalled_Integration(t *testing.T) {
	//
	// Given
	//
	cleanUpCrio(t)

	step, err := SetupCrio().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	//
	// When
	//
	step, err = SetupCrio().Build()

	//
	// Then
	//
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

}

func TestSetupCrio_PartiallyInstalled_Integration(t *testing.T) {
	//
	// Given
	//
	cleanUpCrio(t)

	step, err := SetupCrio().Build()
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
	step, err = SetupCrio().Build()

	//
	// Then
	//
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)
}
