// SPDX-License-Identifier: Apache-2.0

package state

import "path/filepath"

// ComponentID names one of the components whose persisted state lives in its
// own file under the state directory. Refresh/FlushScoped key off this type to
// decide which file to read or write.
type ComponentID string

const (
	// ComponentMachine holds Version, ProvisionerState and MachineState — the
	// fields with no single owning handler. It is written by every handler
	// alongside whatever it manages (see internal/bll.WithManagedComponents).
	ComponentMachine ComponentID = "machine"
	ComponentCluster ComponentID = "cluster"
	// ComponentBlockNode is the block node's persisted state.
	ComponentBlockNode ComponentID = "blocknode"
	// ComponentConsensus is the consensus-node map.
	ComponentConsensus ComponentID = "consensus"
	ComponentTeleport  ComponentID = "teleport"
)

// AllComponentIDs is every component with its own persisted file. Refresh
// reads each of these independently; a missing file leaves that component at
// its zero value, the same as a missing state.yaml did before the split.
var AllComponentIDs = []ComponentID{
	ComponentMachine,
	ComponentCluster,
	ComponentBlockNode,
	ComponentConsensus,
	ComponentTeleport,
}

// ComponentFilePath returns the path of id's persisted file inside dir, for
// callers outside this package that need to check a component's file directly
// (tests mainly; production code should go through Manager).
func ComponentFilePath(dir string, id ComponentID) string {
	return componentFilePath(dir, id)
}

// componentFilePath returns the path of id's persisted file inside dir.
func componentFilePath(dir string, id ComponentID) string {
	return filepath.Join(dir, string(id)+".yaml")
}

// applyComponentSection copies only id's section from src into dst, leaving
// every other field of dst untouched. It is symmetric: used with dst holding
// defaults and src holding a file's content, it overlays that one component
// onto an in-memory State; used with src holding the full in-memory State and
// dst starting zero-valued, it projects out just that component's content for
// writing to its own file.
func applyComponentSection(dst *State, id ComponentID, src State) {
	switch id {
	case ComponentMachine:
		dst.Version = src.Version
		dst.ProvisionerState = src.ProvisionerState
		dst.MachineState = src.MachineState
	case ComponentCluster:
		dst.ClusterState = src.ClusterState
	case ComponentBlockNode:
		dst.BlockNodeState = src.BlockNodeState
	case ComponentConsensus:
		dst.ConsensusNodes = src.ConsensusNodes
	case ComponentTeleport:
		dst.TeleportState = src.TeleportState
	}
}

// projectComponentSection returns a State carrying only id's section of full,
// with every other component at its zero value and the shared envelope
// (StateFile) preserved so the result can be marshaled as that component's file.
func projectComponentSection(full State, id ComponentID) State {
	projected := State{StateFile: full.StateFile}
	applyComponentSection(&projected, id, full)
	return projected
}

// DedupeComponentIDs returns ids with duplicates removed, preserving the
// first occurrence's order. Exported so callers outside this package (e.g.
// internal/bll.BaseHandler, which unions a handler's managed components with
// the machine component) can share one definition of "duplicate" instead of
// each writing their own — AcquireComponentLocks needs this because a caller
// that accidentally lists the same component twice would otherwise try to
// flock it twice in the same call (two separate open file descriptions on
// the same file conflict with each other even within one process, so the
// second attempt would see its own first lock as already held and fail or
// hang on itself); FlushScoped needs it because flushComponents has no
// dedup of its own and would otherwise prepare and write the same file twice.
func DedupeComponentIDs(ids []ComponentID) []ComponentID {
	seen := make(map[ComponentID]struct{}, len(ids))
	deduped := make([]ComponentID, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		deduped = append(deduped, id)
	}
	return deduped
}
