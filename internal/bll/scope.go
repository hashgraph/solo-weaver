// SPDX-License-Identifier: Apache-2.0

package bll

import (
	"github.com/hashgraph/solo-weaver/internal/drift"
	"github.com/hashgraph/solo-weaver/internal/state"
)

// Component is one managed component of the system a handler can own. A handler
// declares the components it manages with WithManagedComponents; that single
// declaration scopes two things:
//
//   - drift detection — only the producers of managed components run, so a
//     command reports out-of-band drift for what it owns, not for everything.
//   - the state flush — internal/state.Writer.FlushScoped writes only the
//     listed components' files; every other component's file is left
//     untouched (not read, not rewritten), so a command never rewrites state
//     it does not own.
//
// Scope governs the write, not the read: a handler may still read any
// component's live state during the in-memory refresh.
type Component struct {
	// id is the internal/state.ComponentID this component corresponds to on
	// disk, used to select which file(s) a flush writes and locks.
	id state.ComponentID
	// producer reports this component's out-of-band drift, or nil when the
	// component has no drift producer yet.
	producer drift.Producer
}

// The managed components. Each pairs a component's internal/state.ComponentID
// (the file a flush writes) with its drift producer, when it has one.
var (
	Cluster   = Component{id: state.ComponentCluster}
	BlockNode = Component{id: state.ComponentBlockNode}
	// ConsensusNode has no drift producer yet; a consensus drift producer will
	// wire one in later. The flush scope applies now regardless.
	ConsensusNode = Component{id: state.ComponentConsensus}
	Teleport      = Component{id: state.ComponentTeleport, producer: drift.Teleport}
)

// producersOf returns the drift producers of the managed components that have one.
func producersOf(managed []Component) []drift.Producer {
	producers := make([]drift.Producer, 0, len(managed))
	for _, c := range managed {
		if c.producer != nil {
			producers = append(producers, c.producer)
		}
	}
	return producers
}

// componentIDsOf returns the internal/state.ComponentID of every managed
// component, for selecting which files a flush writes.
func componentIDsOf(managed []Component) []state.ComponentID {
	ids := make([]state.ComponentID, len(managed))
	for i, c := range managed {
		ids[i] = c.id
	}
	return ids
}
