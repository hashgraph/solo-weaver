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
