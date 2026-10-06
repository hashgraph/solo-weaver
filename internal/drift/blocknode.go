// SPDX-License-Identifier: Apache-2.0

package drift

import (
	"strconv"

	"github.com/hashgraph/solo-weaver/internal/state"
)

const blockNodeComponent = "block node"

// BlockNode reports block node fields that differ between state.yaml and the
// cluster. Weaver-only records are left out: reality cannot read them back.
//
// An install or removal is reported as "installed" alone, since every other
// field follows it; the fields are compared only while both sides have a
// release. state.yaml has no "installed" key: the release name stands for it.
func BlockNode(baseline, live state.State) []Change {
	persisted, observed := baseline.BlockNodeState, live.BlockNodeState
	was, is := hasRelease(persisted), hasRelease(observed)
	if was != is {
		return []Change{{
			Component: blockNodeComponent,
			Field:     "installed",
			Persisted: strconv.FormatBool(was),
			Live:      strconv.FormatBool(is),
		}}
	}
	if !is {
		return nil
	}

	var changes []Change
	for _, d := range persisted.Diff(observed) {
		if d.Observable {
			changes = append(changes, Change{Component: blockNodeComponent, Field: d.Field, Persisted: d.Persisted, Live: d.Reality})
		}
	}
	return changes
}

// hasRelease reports whether b records a block node release, deployed or not.
func hasRelease(b state.BlockNodeState) bool {
	return b.ReleaseInfo.Name != ""
}
