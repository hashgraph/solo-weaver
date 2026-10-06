// SPDX-License-Identifier: Apache-2.0

package bll

import (
	"strings"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/drift"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// detectOutOfBandChanges compares the persisted baseline with the refreshed
// live state using producers. With no baseline (no state file yet) there is
// nothing to compare.
//
// Each change is logged here, so it is on record even if the run fails before
// its report exists. The log is debug only: an Info or Warn line would reach
// the console as well as the report warning, showing the change twice.
func detectOutOfBandChanges(baseline *state.State, live state.State, producers ...drift.Producer) []drift.Change {
	if baseline == nil {
		return nil
	}

	changes := drift.Detect(*baseline, live, producers...)
	for _, c := range changes {
		logx.As().Debug().
			Str("component", c.Component).
			Str("field", c.Field).
			Str("persisted", c.Persisted).
			Str("live", c.Live).
			Msg("Live value differs from persisted state")
	}
	return changes
}

// addOutOfBandWarnings records each change as a report warning, which the
// summary table and the JSON summary both show.
func addOutOfBandWarnings(report *automa.Report, changes []drift.Change) {
	if report == nil || len(changes) == 0 {
		return
	}

	lines := make([]string, 0, len(changes)+1)
	if existing := report.Metadata[models.ReportMetaWarning]; existing != "" {
		lines = append(lines, existing)
	}
	for _, c := range changes {
		lines = append(lines, c.String())
	}

	if report.Metadata == nil {
		report.Metadata = map[string]string{}
	}
	report.Metadata[models.ReportMetaWarning] = strings.Join(lines, "\n")
}
