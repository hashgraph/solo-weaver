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
)
