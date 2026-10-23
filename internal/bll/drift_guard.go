// SPDX-License-Identifier: Apache-2.0

package bll

import (
	"fmt"
	"slices"
	"strings"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/drift"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// guardOwnedDrift stops a destructive action before it destroys owned drift,
// unless --override-drift, --force or a non-interactive run lets it through. A
// part of what it would destroy that this run could not read stops it whatever
// the flags say, and unverified is then true. changes are the run's owned
// out-of-band changes, and destroys reports the ones the action destroys. A run
// that goes ahead logs each change at warn level, and gets the records for its
// action history entry; the flush confirms which were destroyed (see
// ownedDriftRun).
func guardOwnedDrift(
	intent models.Intent,
	common models.CommonInputs,
	destroys func(drift.Change) bool,
	changes []drift.Change,
) ([]state.OwnedDriftRecord, bool, error) {
	var records []state.OwnedDriftRecord
	var stops, unread []drift.Change
	for _, c := range changes {
		// A removal made out of band leaves the action nothing of it to destroy.
		stop := !c.Removed && destroys(c)
		records = append(records, state.OwnedDriftRecord{
			Component: c.Component, Field: c.Field, Persisted: c.Persisted, Live: c.Live,
			DestroyAllowed: stop, Unobserved: c.Unobserved,
		})
		switch {
		case stop && c.Unobserved:
			unread = append(unread, c)
		case stop:
			stops = append(stops, c)
		}
	}
	if len(unread) > 0 {
		return nil, true, driftGuardError(intent, "cannot verify", unread)
	}
	if len(stops) > 0 && !common.OverrideDrift && !common.Force && !common.NonInteractive {
		return nil, false, driftGuardError(intent, "would overwrite out-of-band changes to", stops)
	}

	for _, r := range records {
		l := logx.As().Warn().
			Str("component", r.Component).
			Str("field", r.Field).
			Str("persisted", r.Persisted)
		if r.Unobserved {
			l.Msg("Going ahead over a part that could not be read")
			continue
		}
		l.Str("live", r.Live).Bool("destroyAllowed", r.DestroyAllowed).Msg("Going ahead over an out-of-band change")
	}
	return records, false, nil
}

// ownedDriftRun is what the guard let a run go ahead over, carried to the
// flush: the records, the baseline they were found against, and which changes
// the action destroys.
type ownedDriftRun struct {
	records  []state.OwnedDriftRecord
	baseline *state.State
	destroys func(drift.Change) bool
}

// confirm sets Destroyed on each record the run was allowed to destroy when
// after, the run's own read once the workflow is done, could read that part and
// no longer holds the change: the same field with the same live value. A run
// can fail and still have removed it. With no baseline the records stand.
func (r ownedDriftRun) confirm(after state.State, producers ...drift.Producer) []state.OwnedDriftRecord {
	if len(r.records) == 0 || r.baseline == nil {
		return r.records
	}
	read := drift.Detect(*r.baseline, after, producers...)
	readAfter := !slices.ContainsFunc(read, func(c drift.Change) bool { return c.Unobserved && r.destroys(c) })

	records := slices.Clone(r.records)
	for i := range records {
		rec := &records[i]
		held := slices.ContainsFunc(read, func(c drift.Change) bool {
			return !c.Unobserved && c.Component == rec.Component && c.Field == rec.Field && c.Live == rec.Live
		})
		rec.Destroyed = rec.DestroyAllowed && readAfter && !held
	}
	return records
}

// driftGuardError names each change that stopped the action, one per line, after
// a header saying what the action would do to them: a run stopped before its
// workflow prints no summary to show them.
func driftGuardError(intent models.Intent, what string, changes []drift.Change) error {
	var components []string
	var lines []string
	for _, c := range changes {
		if !slices.Contains(components, c.Component) {
			components = append(components, c.Component)
		}
		lines = append(lines, "  "+c.String())
	}
	header := fmt.Sprintf("%s %s %s %s:", intent.Target, intent.Action, what, strings.Join(components, ", "))
	return errorx.IllegalState.New("%s", strings.Join(append([]string{header}, lines...), "\n"))
}

// driftGuardHint tells the operator how to get past the stop. A part the run
// could not read only needs the live system back: no flag goes past it, and a
// stopped run writes no history entry. Otherwise the override goes ahead, and
// the overwritten values are recorded. dir is the state directory.
func driftGuardHint(dir string, unverified bool) string {
	if unverified {
		return "Re-run once the live system can be read"
	}
	return "Re-run the same command with --override-drift to proceed; the overwritten values are recorded in " +
		state.ActionHistoryFilePath(dir)
}
