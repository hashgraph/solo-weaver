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
//   - the state flush — only managed components' sections are written from
//     reality; every other component's section is reverted to the persisted
//     baseline, so a command never rewrites state it does not own.
//
// Scope governs the write, not the read: a handler may still read any
// component's live state during the in-memory refresh.
type Component struct {
	// name uniquely identifies the component within a handler's managed set.
	name string
	// producer reports this component's out-of-band drift, or nil when the
	// component has no drift producer yet.
	producer drift.Producer
	// restore reverts this component's section of dst to the persisted baseline,
	// undoing the live values a full refresh folded in.
	restore func(dst *state.State, baseline state.State)
}

// The managed components. Each pairs a component's drift producer (when it has
// one) with the rule for reverting its state section to the persisted baseline.
var (
	Cluster = Component{
		name:    "cluster",
		restore: func(dst *state.State, b state.State) { dst.ClusterState = b.ClusterState },
	}
	BlockNode = Component{
		name:    "blocknode",
		restore: func(dst *state.State, b state.State) { dst.BlockNodeState = b.BlockNodeState },
	}
	// ConsensusNode has no drift producer on this branch; drift.ConsensusNode is
	// wired in with #1187. The flush scope applies now regardless.
	ConsensusNode = Component{
		name:    "consensus",
		restore: func(dst *state.State, b state.State) { dst.ConsensusNodes = b.ConsensusNodes },
	}
	Teleport = Component{
		name:     "teleport",
		producer: drift.Teleport,
		restore:  func(dst *state.State, b state.State) { dst.TeleportState = b.TeleportState },
	}
)

// allComponents is every component whose section the flush scopes. A component
// absent from a handler's managed set has its section reverted to the baseline.
var allComponents = []Component{Cluster, BlockNode, ConsensusNode, Teleport}

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

// restoreUnmanaged reverts every component not in managed to its persisted
// baseline value, so the flush writes live values only for managed components.
func restoreUnmanaged(dst *state.State, baseline state.State, managed []Component) {
	owned := make(map[string]struct{}, len(managed))
	for _, c := range managed {
		owned[c.name] = struct{}{}
	}
	for _, c := range allComponents {
		if _, ok := owned[c.name]; !ok {
			c.restore(dst, baseline)
		}
	}
}
