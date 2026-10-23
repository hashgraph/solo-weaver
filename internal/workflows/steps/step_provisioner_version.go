// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/joomcode/errorx"
)

// persistProvisionerVersion records the running CLI version in provisioner.yaml.
// A var so tests can stub it to exercise the step's best-effort swallow
// without a writable state dir.
var persistProvisionerVersion = state.PersistProvisionerVersion

// metaVersionPersisted is a report metadata key: "true" when the step changed
// provisioner.yaml, "false" when the best-effort persist failed, and absent
// when the step did not run or found the version already recorded.
// OnCompletion reads it to emit an honest message instead of unconditionally
// reporting success.
const metaVersionPersisted = "version_persisted"

// metaVersionSynced is a report metadata key: "false" when the step changed
// provisioner.yaml but could not sync it to disk, and absent otherwise.
const metaVersionSynced = "version_synced"

// recordProvisionerVersionStepID is the id of the RecordProvisionerVersion step.
const recordProvisionerVersionStepID = "record-provisioner-version"

// RecordProvisionerVersion persists the running CLI version to provisioner.yaml
// as soon as kubeadm init has created a cluster, whichever command created it.
// The startup pass records it only on a host that already had Kubernetes, so
// without this a fresh cluster has no recorded version and the next invocation
// synthesises the 0.0.0 baseline and re-runs historical migrations.
func RecordProvisionerVersion() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(recordProvisionerVersionStepID).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			changed, err := persistProvisionerVersion()
			if err != nil && errorx.IsOfType(err, fsx.FileSyncError) {
				logx.As().Warn().Err(err).Msg("Recorded provisioner version after cluster install, but could not sync it to disk")
				return automa.StepSuccessReport(stp.Id(),
					automa.WithMetadata(automa.StringMap{metaVersionPersisted: "true", metaVersionSynced: "false"}))
			}
			if err != nil {
				logx.As().Warn().Err(err).
					Msg("Failed to record provisioner version after cluster install; " +
						"startup migrations may re-evaluate historical boundaries on the next invocation")
				// Stay non-fatal (success) so cluster install does not abort, but
				// flag the miss so OnCompletion reports it honestly.
				return automa.StepSuccessReport(stp.Id(),
					automa.WithMetadata(automa.StringMap{metaVersionPersisted: "false"}))
			}
			if !changed {
				return automa.StepSuccessReport(stp.Id())
			}
			return automa.StepSuccessReport(stp.Id(),
				automa.WithMetadata(automa.StringMap{metaVersionPersisted: "true"}))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Recording provisioner version")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to record provisioner version")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			if rpt.Metadata[metaVersionPersisted] == "false" {
				notify.As().StepCompletion(ctx, stp, rpt,
					"Provisioner version not recorded (best-effort); "+
						"startup migrations may re-run on the next invocation")
				return
			}
			if rpt.Metadata[metaVersionSynced] == "false" {
				notify.As().StepCompletion(ctx, stp, rpt, "Provisioner version recorded, but not synced to disk")
				return
			}
			notify.As().StepCompletion(ctx, stp, rpt, "Provisioner version recorded")
		})
}

// ProvisionerVersionPersisted reports whether the RecordProvisionerVersion step
// in report's workflow wrote provisioner.yaml, so the command can name it among
// the files its action history entry changed. Rollback reports are not
// searched: a rollback never records the version.
func ProvisionerVersionPersisted(report *automa.Report) bool {
	return versionPersistedValue(report) == "true"
}

// ProvisionerVersionFailed reports whether the RecordProvisionerVersion step in
// report's workflow failed to write provisioner.yaml, so the command can mark
// its entry NotPersisted. A version written but not synced is not a failure.
func ProvisionerVersionFailed(report *automa.Report) bool {
	return versionPersistedValue(report) == "false"
}

// versionPersistedValue returns the RecordProvisionerVersion step's
// metaVersionPersisted value in report's workflow, or "" when the step did not
// run or recorded nothing.
func versionPersistedValue(report *automa.Report) string {
	if report == nil {
		return ""
	}
	if report.Id == recordProvisionerVersionStepID {
		if v := report.Metadata[metaVersionPersisted]; v != "" {
			return v
		}
	}
	for _, sr := range report.StepReports {
		if v := versionPersistedValue(sr); v != "" {
			return v
		}
	}
	return ""
}
