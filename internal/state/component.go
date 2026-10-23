// SPDX-License-Identifier: Apache-2.0

package state

import "path/filepath"

// ComponentID names one of the components whose persisted state lives in its
// own file under the state directory. Refresh/Commit key off this type to
// decide which file to read or write.
type ComponentID string

const (
	// ComponentProfile is the host's deployment profile, set by the installs.
	ComponentProfile ComponentID = "profile"
	// ComponentFirewall is the host's last host-firewall decision. It is
	// host-scoped, so it survives a block-node uninstall.
	ComponentFirewall ComponentID = "firewall"
	// ComponentSystem is what the host has installed (Software) and runs on
	// (Hardware), written by the commands that install host binaries.
	ComponentSystem  ComponentID = "system"
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
	ComponentProfile,
	ComponentFirewall,
	ComponentSystem,
	ComponentCluster,
	ComponentBlockNode,
	ComponentConsensus,
	ComponentTeleport,
}

// ChangedFile names a state file in ActionHistory.Components: a component's
// file, or one of the files below that belong to no component.
type ChangedFile string

const (
	// ChangedProvisioner is provisioner.yaml.
	ChangedProvisioner ChangedFile = "provisioner"
	// ChangedLegacy is the pre-split state: state.yaml, its .legacy and
	// .diverged-* copies, and the *.installed / *.configured marker files.
	ChangedLegacy ChangedFile = "legacy"
)

// File returns id's file as recorded in the action history.
func (id ComponentID) File() ChangedFile { return ChangedFile(id) }

// ComponentFilePath returns the path of id's persisted file inside dir, for
// callers outside this package that need to check a component's file directly
// (tests mainly; production code should go through Manager).
func ComponentFilePath(dir string, id ComponentID) string {
	return componentFilePath(dir, id)
}

// ActionHistoryFilePath returns the path of the action history inside dir.
func ActionHistoryFilePath(dir string) string {
	return filepath.Join(dir, "action_history.yaml")
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
	case ComponentProfile:
		dst.MachineState.Profile = src.MachineState.Profile
	case ComponentFirewall:
		dst.MachineState.Firewall = src.MachineState.Firewall
	case ComponentSystem:
		dst.MachineState.Software = src.MachineState.Software
		dst.MachineState.Hardware = src.MachineState.Hardware
		dst.MachineState.LastSync = src.MachineState.LastSync
	case ComponentCluster:
		dst.ClusterState = src.ClusterState
	case ComponentBlockNode:
		dst.BlockNodeState = src.BlockNodeState
	case ComponentConsensus:
		dst.ConsensusNodes = src.ConsensusNodes
		dst.ConsensusOrbits = src.ConsensusOrbits
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
// internal/bll.BaseHandler, whose managed components may repeat) can share
// one definition of "duplicate" instead of each writing their own.
// AcquireComponentLocks needs this because a caller that lists the same
// component twice would otherwise flock it twice in the same call (two open
// file descriptions on the same file conflict even within one process, so
// the second attempt would see its own first lock as held and fail or hang
// on itself); Commit needs it because flushComponents has no dedup of
// its own and would otherwise prepare and write the same file twice.
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
