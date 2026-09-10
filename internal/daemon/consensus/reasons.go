// SPDX-License-Identifier: Apache-2.0

package consensus

import "github.com/automa-saga/errx"

// Reason codes for the execute-phase workflow. Each is emitted on both channels —
// the per-operation JSONL audit line and the paired structured log line — and the
// string values match the HIP-1496 event names exactly. Typed so the vocabulary is
// enumerable and typo-safe at call sites. Follow-up stories add the per-step
// reasons they emit.
const (
	ReasonExecuteWorkflowStarted   errx.Reason = "ExecuteWorkflowStarted"
	ReasonExecuteWorkflowCompleted errx.Reason = "ExecuteWorkflowCompleted"
	ReasonExecuteWorkflowFailed    errx.Reason = "ExecuteWorkflowFailed"

	// ReasonPendingInfraUpgrade is emitted when the daemon writes the durable
	// PendingInfraUpgrade phase before an infra upgrade. Matches cn.PhasePendingInfraUpgrade.
	ReasonPendingInfraUpgrade errx.Reason = "PendingInfraUpgrade"

	// ReasonExecuteWorkflowRetrying is emitted (WARN) when a transient failure is
	// left to retry via watch re-delivery, within the handoff deadline — so the
	// operation does not silently stall (HIP-1496 "SHOULD emit a warning event").
	ReasonExecuteWorkflowRetrying errx.Reason = "ExecuteWorkflowRetrying"

	// Config-CR deployment reasons (create + wait steps). Audit labels on the JSONL
	// and logx channels; not wire values — except ReasonConfigCRInvalid, which is
	// surfaced verbatim as the DaemonResult reason and so MUST match the operator's
	// HIP-1496 reason "ConfigCRInvalid".
	ReasonConfigDirAbsent        errx.Reason = "ConfigDirAbsent"
	ReasonConfigFileIgnored      errx.Reason = "ConfigFileIgnored"
	ReasonConfigFileUnrecognised errx.Reason = "ConfigFileUnrecognised"
	ReasonConfigFileSymlink      errx.Reason = "ConfigFileSymlink"
	ReasonConfigFileTooLarge     errx.Reason = "ConfigFileTooLarge"
	ReasonUpgradePathUnreadable  errx.Reason = "UpgradePathUnreadable"
	ReasonConfigCRCreated        errx.Reason = "ConfigCRCreated"
	ReasonConfigCRExists         errx.Reason = "ConfigCRExists"
	ReasonConfigCRCreateFailed   errx.Reason = "ConfigCRCreateFailed"
	ReasonConfigCRGetFailed      errx.Reason = "ConfigCRGetFailed"
	ReasonConfigCRValid          errx.Reason = "ConfigCRValid"
	ReasonConfigCRInvalid        errx.Reason = "ConfigCRInvalid"
	ReasonConfigCRWaitTimeout    errx.Reason = "ConfigCRWaitTimeout"

	// infrastructure-versions.yaml placement reasons.
	ReasonInfraVersionsPlaced      errx.Reason = "InfraVersionsPlaced"
	ReasonInfraVersionsBackedUp    errx.Reason = "InfraVersionsBackedUp"
	ReasonInfraVersionsAbsent      errx.Reason = "InfraVersionsAbsent"
	ReasonInfraVersionsPlaceFailed errx.Reason = "InfraVersionsPlaceFailed"
)
