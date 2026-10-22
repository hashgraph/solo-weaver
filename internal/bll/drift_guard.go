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
// unless --override-drift, --force or a non-interactive run lets it through.
// changes are the run's owned out-of-band changes, and destroys reports the
// ones the action destroys. A run that goes ahead logs each change at warn
// level, and gets the records for its action history entry.
func guardOwnedDrift(
	intent models.Intent,
	common models.CommonInputs,
	destroys func(drift.Change) bool,
	changes []drift.Change,
) ([]state.OwnedDriftRecord, error) {
	var records []state.OwnedDriftRecord
	var destroyed []drift.Change
	for _, c := range changes {
		// A removal made out of band leaves the action nothing of it to destroy.
		d := !c.Removed && destroys(c)
		records = append(records, state.OwnedDriftRecord{
			Component: c.Component, Field: c.Field, Persisted: c.Persisted, Live: c.Live, Destroyed: d,
		})
		if d {
			destroyed = append(destroyed, c)
		}
	}
	if len(destroyed) > 0 && !common.OverrideDrift && !common.Force && !common.NonInteractive {
		return nil, driftGuardError(intent, destroyed)
	}

	for _, r := range records {
		logx.As().Warn().
			Str("component", r.Component).
			Str("field", r.Field).
			Str("persisted", r.Persisted).
			Str("live", r.Live).
			Bool("destroyed", r.Destroyed).
			Msg("Going ahead over an out-of-band change")
	}
	return records, nil
}

// driftGuardError names each change the action would destroy, one per line:
// a run stopped before its workflow prints no summary to show them.
func driftGuardError(intent models.Intent, destroyed []drift.Change) error {
	var components []string
	var lines []string
	for _, c := range destroyed {
		if !slices.Contains(components, c.Component) {
			components = append(components, c.Component)
		}
		lines = append(lines, "  "+c.String())
	}
	header := fmt.Sprintf("%s %s would overwrite out-of-band changes to %s:",
		intent.Target, intent.Action, strings.Join(components, ", "))
	return errorx.IllegalState.New("%s", strings.Join(append([]string{header}, lines...), "\n"))
}

// driftGuardHint tells the operator how to go ahead anyway, and where the
// overwritten values are then recorded. dir is the state directory.
func driftGuardHint(dir string) string {
	return "Re-run the same command with --override-drift to proceed; the overwritten values are recorded in " +
		state.ActionHistoryFilePath(dir)
}
