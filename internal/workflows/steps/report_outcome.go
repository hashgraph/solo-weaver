// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/state"
)

// OutcomeOf says what a workflow did, for its action history entry: succeeded,
// or failed, rollback or not. A nil report reads as failed: nothing reported
// an outcome.
func OutcomeOf(report *automa.Report) state.Outcome {
	if report == nil || report.Error != nil || report.Status == automa.StatusFailed {
		return state.OutcomeFailed
	}
	return state.OutcomeSucceeded
}
