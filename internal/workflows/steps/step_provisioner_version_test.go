// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/version"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRecordProvisionerVersion_WritesStateFile: the Kubernetes setup tail step
// writes a provisioner.yaml holding the running binary's version, which the
// next invocation's reader then observes instead of synthesising the 0.0.0
// baseline.
func TestRecordProvisionerVersion_WritesStateFile(t *testing.T) {
	home := t.TempDir()
	t.Cleanup(models.SetPaths(home))
	// A provisioned host has the state dir (created at install) but no recorded version.
	require.NoError(t, os.MkdirAll(models.Paths().StateDir, 0o755))

	provisionerFile := filepath.Join(models.Paths().StateDir, state.ProvisionerFileName)
	_, statErr := os.Stat(provisionerFile)
	require.True(t, os.IsNotExist(statErr), "precondition: provisioner.yaml must not exist yet")

	step, err := RecordProvisionerVersion().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error, "step must succeed")

	_, statErr = os.Stat(provisionerFile)
	require.NoError(t, statErr, "provisioner.yaml should now exist")

	recorded, err := state.ReadProvisionerVersionFromDisk()
	require.NoError(t, err)
	assert.Equal(t, version.Get().Version, recorded,
		"the reader must observe the version the step persisted")
}

// TestRecordProvisionerVersion_PersistFailureIsNonFatal: recording the version is
// best-effort — a failure is logged, not fatal. The cluster install must not fail
// just because provisioner.yaml could not be written. Regression guard: returning the
// error here (instead of swallowing it) would abort `kube cluster install`.
func TestRecordProvisionerVersion_PersistFailureIsNonFatal(t *testing.T) {
	origPersist := persistProvisionerVersion
	t.Cleanup(func() { persistProvisionerVersion = origPersist })
	persistCalled := 0
	persistProvisionerVersion = func() (bool, error) {
		persistCalled++
		return false, errors.New("simulated persist failure")
	}

	step, err := RecordProvisionerVersion().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error, "a failed version record must not fail the step")
	assert.Equal(t, 1, persistCalled, "the version record must have been attempted")
}

// TestRecordProvisionerVersion_CompletionMessageReflectsPersistOutcome: the step
// stays non-fatal on persist failure, but the completion message must not claim
// success. On failure it flags the miss via report metadata and OnCompletion
// emits a distinct, honest message; on success it reports the version recorded.
func TestRecordProvisionerVersion_CompletionMessageReflectsPersistOutcome(t *testing.T) {
	origHandler := notify.As().StepCompletion
	t.Cleanup(func() { notify.SetDefault(&notify.Handler{StepCompletion: origHandler}) })

	var gotMsg string
	notify.SetDefault(&notify.Handler{
		StepCompletion: func(_ context.Context, _ automa.Step, _ *automa.Report, msg string, _ ...interface{}) {
			gotMsg = msg
		},
	})

	origPersist := persistProvisionerVersion
	t.Cleanup(func() { persistProvisionerVersion = origPersist })

	// Failure path: the completion message must signal the miss.
	persistProvisionerVersion = func() (bool, error) {
		return false, errors.New("simulated persist failure")
	}
	step, err := RecordProvisionerVersion().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NoError(t, report.Error, "persist failure must remain non-fatal")
	assert.Equal(t, "false", report.Metadata[metaVersionPersisted],
		"the report must flag that the version was not persisted")
	assert.Contains(t, gotMsg, "not recorded",
		"the completion message must not claim the version was recorded")

	// Success path: the completion message reports the version recorded, and
	// the report says it changed the file only when it did.
	for _, changed := range []bool{true, false} {
		gotMsg = ""
		persistProvisionerVersion = func() (bool, error) { return changed, nil }
		step, err = RecordProvisionerVersion().Build()
		require.NoError(t, err)
		report = step.Execute(context.Background())
		require.NoError(t, report.Error)
		assert.NotEqual(t, "false", report.Metadata[metaVersionPersisted],
			"a successful persist must not flag a miss")
		assert.Equal(t, changed, ProvisionerVersionPersisted(report),
			"version_persisted is true only when the file changed")
		assert.Equal(t, "Provisioner version recorded", gotMsg)
	}
}

// TestRecordProvisionerVersion_UnsyncedVersionIsRecorded: a version written but
// not synced counts as recorded, and the completion line says it is not synced,
// since in TUI mode the step's log line reaches only the log file.
func TestRecordProvisionerVersion_UnsyncedVersionIsRecorded(t *testing.T) {
	origHandler := notify.As().StepCompletion
	t.Cleanup(func() { notify.SetDefault(&notify.Handler{StepCompletion: origHandler}) })
	var gotMsg string
	notify.SetDefault(&notify.Handler{
		StepCompletion: func(_ context.Context, _ automa.Step, _ *automa.Report, msg string, _ ...interface{}) {
			gotMsg = msg
		},
	})
	origPersist := persistProvisionerVersion
	t.Cleanup(func() { persistProvisionerVersion = origPersist })
	persistProvisionerVersion = func() (bool, error) { return true, fsx.FileSyncError.New("disk gone") }

	step, err := RecordProvisionerVersion().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())

	require.NoError(t, report.Error)
	assert.True(t, ProvisionerVersionPersisted(report), "the version is on disk")
	assert.Equal(t, "Provisioner version recorded, but not synced to disk", gotMsg)
}
