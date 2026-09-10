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
// Writer ownership (HIP-1496 — see docs/dev/execute-phase-handshake.md):
//
//   - The operator writes Pending, ReadyForProvisionerDaemon, and the terminal
//     Succeeded/Failed.
//   - The daemon writes PendingInfraUpgrade (before infra-mutating work, only when
//     an infra upgrade is required — the durable checkpoint that survives a cluster
//     teardown, during which the operator does not exist) and PendingNodeUpgrade
//     (on success). It also sets two conditions: ConfigCRsApplied (after config CRs
//     reconcile) and DaemonResult (True/reason Succeeded, or False/reason on
//     failure); the operator reads DaemonResult to decide the terminal phase. There
//     is no InProgress phase.
//
// State machine:
//
//	Pending ─▶ ReadyForProvisionerDaemon ─▶ [PendingInfraUpgrade ─▶] PendingNodeUpgrade ─▶ Succeeded
//	 (operator)      (operator)              (daemon, if infra)        (daemon) │          (operator)
//	                                                              DaemonResult=False
//	                                                                            ▼
//	                                                                          Failed (operator)
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

// The two status conditions the daemon writes on a NetworkUpgradeExecute CR; the
// operator reads them to drive phase transitions (it owns every phase). Values
// match the operator constants exactly.
const (
	// DaemonResultCondition gates ReadyForProvisionerDaemon → PendingInfraUpgrade
	// (True) or → Failed (False).
	DaemonResultCondition ConditionType = "DaemonResult"

	// ConfigCRsAppliedCondition gates PendingInfraUpgrade → PendingNodeUpgrade once
	// the daemon has applied the upgrade package's config CRs.
	ConfigCRsAppliedCondition ConditionType = "ConfigCRsApplied"
)

// ConditionStatus mirrors metav1.ConditionStatus values.
type ConditionStatus string

const (
	ConditionTrue  ConditionStatus = "True"
	ConditionFalse ConditionStatus = "False"
)

// ConditionReason is the reason code stamped on a daemon-written condition. Values
// match the operator's daemon reason vocabulary.
type ConditionReason string

// These mirror the operator's daemon reason vocabulary in
// solo-operator api/v1alpha1 (networkupgrade_common_types.go, the ReasonDaemon*
// errx.Reason values) byte-for-byte — the ExecuteReconciler reads DaemonResult's
// reason, so any divergence breaks the handshake.
const (
	// ReasonDaemonSucceeded marks a successful condition (DaemonResult=True,
	// ConfigCRsApplied=True).
	ReasonDaemonSucceeded ConditionReason = "Succeeded"

	// ReasonDaemonFileDownloadFailed / ReasonDaemonFileHashMismatch report a fatal
	// external-files failure on DaemonResult=False.
	ReasonDaemonFileDownloadFailed ConditionReason = "FileDownloadFailed"
	ReasonDaemonFileHashMismatch   ConditionReason = "FileHashMismatch"

	// ReasonDaemonInfraUpgradeFailed reports a fatal infra-upgrade failure. It is
	// also the generic fallback reason for an unclassified fatal execute failure,
	// mirroring the UC provisioner-proxy's fallback (provisioner_proxy.go).
	ReasonDaemonInfraUpgradeFailed ConditionReason = "InfraUpgradeFailed"

	// ReasonDaemonSelfUpgradeFailed reports a fatal daemon self-upgrade failure.
	ReasonDaemonSelfUpgradeFailed ConditionReason = "SelfUpgradeFailed"

	// ReasonDaemonDeadlineExceeded is written on DaemonResult=False when the handoff
	// deadline (anchored to status.startTime) elapses before a terminal outcome, so
	// a stuck-transient operation terminates rather than hanging (HIP-1496).
	ReasonDaemonDeadlineExceeded ConditionReason = "DeadlineExceeded"
)

// IsTerminal reports whether p is a terminal phase. Terminal phases are written
// ONLY by the reconciler; the daemon must never write them.
func (p Phase) IsTerminal() bool {
	return p == PhaseSucceeded || p == PhaseFailed
}

// IsDaemonWritable reports whether the daemon is permitted to write p. Per HIP-1496
// the daemon writes exactly two phases: the durable infra checkpoint
// PendingInfraUpgrade and the success handoff PendingNodeUpgrade.
func (p Phase) IsDaemonWritable() bool {
	return p == PhasePendingInfraUpgrade || p == PhasePendingNodeUpgrade
}
