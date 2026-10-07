// SPDX-License-Identifier: Apache-2.0

package models

import "github.com/automa-saga/automa"

func AllExecutionModes() []automa.TypeMode {
	return []automa.TypeMode{automa.RollbackOnError, automa.StopOnError, automa.ContinueOnError}
}

// ReportMetaWarning is the automa.Report metadata key for a step's non-fatal,
// operator-facing warning. Lives here, not in internal/workflows/steps, so
// internal/ui can read it without importing the steps package.
const ReportMetaWarning = "warning"

// ReportMetaStorageMedia is the report metadata key for the newline-separated,
// per-volume storage media lines shown on the console.
const ReportMetaStorageMedia = "storage.media"
