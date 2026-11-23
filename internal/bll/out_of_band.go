// SPDX-License-Identifier: Apache-2.0

package bll

import (
	"slices"
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
// Each change is logged here at debug level, so it is on record even if the run
// fails before its report exists. A run that goes ahead over owned drift also
// logs each change at warn level, before its workflow (guardOwnedDrift).
func detectOutOfBandChanges(baseline *state.State, live state.State, producers ...drift.Producer) []drift.Change {
	if baseline == nil {
		return nil
	}

	changes := drift.Detect(*baseline, live, producers...)
	for _, c := range changes {
		l := logx.As().Debug().
			Str("component", c.Component).
			Str("field", c.Field).
			Str("persisted", c.Persisted)
		if c.Unobserved {
			l.Msg("Live value could not be read")
			continue
		}
		l.Str("live", c.Live).Msg("Live value differs from persisted state")
	}
	return changes
}

// addOutOfBandWarnings records each change as a report warning, which the
// summary table and the JSON summary both show.
func addOutOfBandWarnings(report *automa.Report, changes []drift.Change) {
	lines := make([]string, 0, len(changes))
	for _, c := range changes {
		lines = append(lines, c.String())
	}
	addWarnings(report, lines)
}

// addUnreadAfterRunWarnings warns once for each component whose live state the
// flush could not read, so the record it keeps may predate the run's own
// changes. kept reports the changes on parts the handler keeps at baseline,
// which are not this run's to write; it may be nil.
func addUnreadAfterRunWarnings(report *automa.Report, changes []drift.Change, kept func(drift.Change) bool) {
	var lines []string
	var components []string
	for _, c := range changes {
		if !c.Unobserved || (kept != nil && kept(c)) || slices.Contains(components, c.Component) {
			continue
		}
		components = append(components, c.Component)
		logx.As().Warn().Str("component", c.Component).
			Msg("Could not read the live state after the run; the record kept may predate this run's changes")
		lines = append(lines, c.Component+
			" could not be read after the run; its recorded state may predate this run's changes, which the next run may report as out-of-band")
	}
	addWarnings(report, lines)
}

// addWarnings appends lines to the report's warning, which the summary table
// and the JSON summary both show.
func addWarnings(report *automa.Report, lines []string) {
	if report == nil || len(lines) == 0 {
		return
	}
	if existing := report.Metadata[models.ReportMetaWarning]; existing != "" {
		lines = append([]string{existing}, lines...)
	}
	if report.Metadata == nil {
		report.Metadata = map[string]string{}
	}
	report.Metadata[models.ReportMetaWarning] = strings.Join(lines, "\n")
}

// baselineOrEmpty is the baseline, or an empty state when there is none yet.
func baselineOrEmpty(baseline *state.State) state.State {
	if baseline == nil {
		return state.State{}
	}
	return *baseline
}
