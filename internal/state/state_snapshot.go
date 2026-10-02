// SPDX-License-Identifier: Apache-2.0

package state

import (
	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
)

// PersistedSnapshot returns s as state.yaml would record it, decoded into a
// fresh State that shares no pointers, maps or slices with s.
//
// Use it to keep a baseline across a Refresh: Refresh decodes onto a Clone, and
// Clone is shallow for some nested records, so a plain copy of State() can change
// underneath its holder. A field with no state.yaml form is not carried over.
func (s State) PersistedSnapshot() (State, error) {
	b, err := yaml.Marshal(s)
	if err != nil {
		return State{}, errx.WithReason(
			errorx.InternalError.Wrap(err, "failed to marshal state for snapshot"), reasons.Internal)
	}

	var snapshot State
	if err := yaml.Unmarshal(b, &snapshot); err != nil {
		return State{}, errx.WithReason(
			errorx.InternalError.Wrap(err, "failed to unmarshal state snapshot"), reasons.Internal)
	}
	return snapshot, nil
}
