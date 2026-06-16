// SPDX-License-Identifier: Apache-2.0

// Package consensus holds contracts shared across the consensus-node tracks:
// the daemon-side execute-phase workflow (internal/daemon/consensus, epic #502)
// and the CLI-side recover/self-upgrade stories (epic #500). Keeping these
// definitions in one low-level package lets both tracks import a single source
// of truth without an import cycle.
//
// Import rule: no file may import both this package (internal/consensus) and
// internal/daemon/consensus — they share the package name "consensus" and are
// distinct layers. Daemon implementation files live in internal/daemon/consensus
// and import this package (aliased); CLI/BLL files import this package directly.
package consensus

// Phase is a NetworkUpgradeExecute CR status.phase value, as defined in the CRD.
//
// Writer ownership (finalized in #706):
//
//   - The reconciler is the SOLE writer of the terminal phases Succeeded and
//     Failed. The daemon never writes them.
//   - The daemon writes only PendingInfraUpgrade (the durable crash-recovery
//     resume point, persisted before any infra-mutating work) and the final
//     PendingNodeUpgrade (written after it sets the DaemonResult condition).
//   - There is no InProgress phase. Progress is not modelled as a phase; the
//     daemon's terminal outcome is communicated via the DaemonResult condition,
//     and the reconciler maps that to Succeeded/Failed.
//
// State machine (see docs/dev/upgrade-contracts.md for the full diagram):
//
//	Pending ──▶ ReadyForProvisionerDaemon ──▶ PendingInfraUpgrade ──▶ PendingNodeUpgrade ──▶ Succeeded
//	  (reconciler)        (reconciler)            (daemon, durable)        (daemon)            (reconciler)
//	                                                                            │
//	                                                              DaemonResult=False │
//	                                                                            ▼
//	                                                                          Failed (reconciler)
type Phase string

const (
	// PhasePending is the initial phase set by the reconciler when the CR is created.
	PhasePending Phase = "Pending"

	// PhaseReadyForProvisionerDaemon is set by the reconciler to hand the operation
	// to the daemon. The daemon's upgrade monitor triggers handleExecute on this phase.
	PhaseReadyForProvisionerDaemon Phase = "ReadyForProvisionerDaemon"

	// PhasePendingInfraUpgrade is written by the daemon — durably, in etcd —
	// before it performs any infra-mutating work. It is the single crash-recovery
	// anchor: on restart the daemon re-reads the CR, sees this phase, and resumes
	// the infra upgrade from here (#709 / #717).
	PhasePendingInfraUpgrade Phase = "PendingInfraUpgrade"

	// PhasePendingNodeUpgrade is the daemon's final phase write. The daemon sets
	// the DaemonResult condition (True/False) and then transitions the CR to this
	// phase, handing control back to the reconciler.
	PhasePendingNodeUpgrade Phase = "PendingNodeUpgrade"

	// PhaseSucceeded is a terminal phase written ONLY by the reconciler.
	PhaseSucceeded Phase = "Succeeded"

	// PhaseFailed is a terminal phase written ONLY by the reconciler.
	PhaseFailed Phase = "Failed"
)

// ConditionType is a NetworkUpgradeExecute CR status condition type.
type ConditionType string

// DaemonResultCondition is the condition the daemon sets to report the outcome
// of the execute phase back to the reconciler. Its status (True/False) is the
// terminal handshake signal; the reconciler reads it to decide whether to write
// PhaseSucceeded or PhaseFailed.
const DaemonResultCondition ConditionType = "DaemonResult"

// ConditionStatus mirrors metav1.ConditionStatus values used on the DaemonResult condition.
type ConditionStatus string

const (
	// ConditionTrue on DaemonResult means the daemon completed the execute phase successfully.
	ConditionTrue ConditionStatus = "True"

	// ConditionFalse on DaemonResult means the daemon's execute phase failed
	// (including a recovered panic). The reconciler maps this to PhaseFailed.
	ConditionFalse ConditionStatus = "False"
)

// IsTerminal reports whether p is a terminal phase. Terminal phases are written
// ONLY by the reconciler; the daemon must never write them.
func (p Phase) IsTerminal() bool {
	return p == PhaseSucceeded || p == PhaseFailed
}

// IsDaemonWritable reports whether the daemon is permitted to write p. The
// daemon writes exactly two phases: the durable resume anchor PendingInfraUpgrade
// and the handshake-completing PendingNodeUpgrade.
func (p Phase) IsDaemonWritable() bool {
	return p == PhasePendingInfraUpgrade || p == PhasePendingNodeUpgrade
}
