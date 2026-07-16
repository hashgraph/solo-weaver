// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/version"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// the cluster install tail step writes a state.yaml whose provisioner.version is
// the running binary's version, which the next invocation's reader then observes
// instead of synthesising the 0.0.0 baseline.
func TestRecordProvisionerVersion_WritesStateFile(t *testing.T) {
	home := t.TempDir()
	t.Cleanup(models.SetPaths(home))
	// A provisioned host has the state dir (created at install) but no state.yaml.
	require.NoError(t, os.MkdirAll(models.Paths().StateDir, 0o755))

	stateFile := filepath.Join(models.Paths().StateDir, state.StateFileName)
	_, statErr := os.Stat(stateFile)
	require.True(t, os.IsNotExist(statErr), "precondition: state file must not exist yet")

	step, err := RecordProvisionerVersion().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error, "step must succeed")

	_, statErr = os.Stat(stateFile)
	require.NoError(t, statErr, "state file should now exist")

	recorded, err := state.ReadProvisionerVersionFromDisk()
	require.NoError(t, err)
	assert.Equal(t, version.Get().Version, recorded,
		"the reader must observe the version the step persisted")
}

// TestRecordProvisionerVersion_PersistFailureIsNonFatal: recording the version is
// best-effort — a failure is logged, not fatal. The cluster install must not fail
// just because state.yaml could not be written. Regression guard: returning the
// error here (instead of swallowing it) would abort `kube cluster install`.
func TestRecordProvisionerVersion_PersistFailureIsNonFatal(t *testing.T) {
	origPersist := persistProvisionerVersion
	t.Cleanup(func() { persistProvisionerVersion = origPersist })
	persistCalled := 0
	persistProvisionerVersion = func(_ ...state.ManagerOption) error {
		persistCalled++
		return errors.New("simulated persist failure")
	}

	step, err := RecordProvisionerVersion().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error, "a failed version record must not fail the step")
	assert.Equal(t, 1, persistCalled, "the version record must have been attempted")
}
