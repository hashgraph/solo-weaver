// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"context"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/daemonkit/eventlog"
	"github.com/automa-saga/logx"
	cn "github.com/hashgraph/solo-weaver/internal/consensus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

const executeWorkflowID = "consensus-execute"

// Per-step timeouts. Values are provisional — each follow-up tunes its step's
// budget as it implements the body.
const (
	stepTimeoutExternalFiles   = 30 * time.Minute
	stepTimeoutInfraVersions   = 2 * time.Minute
	stepTimeoutSafetyGate      = 2 * time.Minute
	stepTimeoutInfraDetect     = 5 * time.Minute
	stepTimeoutCreateConfigCRs = 5 * time.Minute
	stepTimeoutWaitReconcile   = 15 * time.Minute
)

// upgradeExecutor orchestrates one NetworkUpgradeExecute operation: it runs the
// execute-phase workflow for a single CR and reports the outcome via status
// conditions. A future migrate flow gets its own executor.
type upgradeExecutor struct {
	client    dynamic.Interface
	namespace string
	crName    string
	sink      *eventSink
}

// run executes the workflow, then reports the outcome to the operator via
// conditions (the operator owns phase transitions). A condition-write failure is
// returned so the operation is retried — the operator cannot advance without it.
func (x *upgradeExecutor) run(ctx context.Context) error {
	workflowErr := runExecuteWorkflow(ctx, x.sink, x.buildWorkflow())
	if condErr := x.reportOutcome(ctx, workflowErr); condErr != nil {
		return condErr
	}
	return workflowErr
}

// buildWorkflow assembles the execute-phase steps in their required order. Each body
// is a stub today (upgrade_steps.go); this ordering and the per-step timeouts are
// the contract. StopOnError halts at the first failing step. The terminal handshake
// (DaemonResult / ConfigCRsApplied conditions) is reportOutcome, not a step, because
// it reports the whole workflow's outcome.
func (x *upgradeExecutor) buildWorkflow() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().
		WithId(executeWorkflowID).
		WithExecutionMode(automa.StopOnError).
		Steps(
			stepExternalFiles("external-files", stepTimeoutExternalFiles),
			stepInfraVersionsPlacement("infra-versions-placement", stepTimeoutInfraVersions),
			stepRuntimeSafetyGate("runtime-safety-gate", stepTimeoutSafetyGate),
			stepInfraUpgradeDetect("infra-upgrade-detect", stepTimeoutInfraDetect, x.client, x.namespace, x.crName, x.sink),
			stepCreateConfigCRs("create-config-crs", stepTimeoutCreateConfigCRs),
			stepWaitConfigReconcile("wait-config-reconcile", stepTimeoutWaitReconcile),
		)
}

// reportOutcome completes the HIP-1496 handshake. On success it sets
// ConfigCRsApplied=True and DaemonResult=True, then advances the phase to
// PendingNodeUpgrade (the daemon owns that phase). On failure it sets
// DaemonResult=False and does not advance the phase — the operator marks the CR
// Failed from the condition. Idempotent.
func (x *upgradeExecutor) reportOutcome(ctx context.Context, workflowErr error) error {
	if workflowErr != nil {
		return x.setCondition(ctx, cn.DaemonResultCondition, cn.ConditionFalse, cn.ReasonDaemonExecuteFailed, workflowErr.Error())
	}
	if err := x.setCondition(ctx, cn.ConfigCRsAppliedCondition, cn.ConditionTrue, cn.ReasonDaemonSucceeded, "config CRs applied"); err != nil {
		return err
	}
	if err := x.setCondition(ctx, cn.DaemonResultCondition, cn.ConditionTrue, cn.ReasonDaemonSucceeded, "execute phase succeeded"); err != nil {
		return err
	}
	// Advance last, after both conditions are set, so the operator observes a
	// complete handshake when it sees PendingNodeUpgrade.
	if err := patchExecutePhase(ctx, x.client, x.namespace, x.crName, cn.PhasePendingNodeUpgrade); err != nil {
		logx.As().Error().Err(err).Str("cr_name", x.crName).
			Msg("failed to advance phase to PendingNodeUpgrade — operator cannot proceed, will retry")
		return err
	}
	return nil
}

func (x *upgradeExecutor) setCondition(ctx context.Context, condType cn.ConditionType, status cn.ConditionStatus, reason cn.ConditionReason, message string) error {
	if err := setExecuteCondition(ctx, x.client, x.namespace, x.crName, condType, status, reason, message); err != nil {
		logx.As().Error().Err(err).
			Str("cr_name", x.crName).
			Str("condition", string(condType)).
			Msg("failed to set execute condition — operator cannot advance, will retry")
		return err
	}
	return nil
}

// runExecute drives one execute-phase operation: it opens the per-operation JSONL
// log, runs the workflow with dual-channel lifecycle events and the condition
// handshake, then closes the log and prunes stale logs.
func (um *UpgradeMonitor) runExecute(ctx context.Context, cr *unstructured.Unstructured, operationID string) error {
	sink := newEventSink(um.openUpgradeEventLog(operationID), um.cfg.NodeID, operationID)
	defer func() {
		sink.close()
		// Prune here too: a long-running daemon never re-runs the prune at Run() start.
		um.pruneUpgradeEventLogs()
	}()

	x := &upgradeExecutor{
		client:    um.client,
		namespace: um.cfg.Namespace,
		crName:    cr.GetName(),
		sink:      sink,
	}
	return x.run(ctx)
}

// runExecuteWorkflow runs wb, emitting the started/completed/failed lifecycle events.
// Split out so tests can drive the event contract with an injected workflow.
func runExecuteWorkflow(ctx context.Context, sink *eventSink, wb *automa.WorkflowBuilder) error {
	sink.info(ReasonExecuteWorkflowStarted, "execute-phase workflow started")
	report := automa.RunWorkflow(ctx, wb)
	if report.HasError() {
		sink.error(ReasonExecuteWorkflowFailed, report.Error.Error())
		return report.Error
	}
	sink.info(ReasonExecuteWorkflowCompleted, "execute-phase workflow completed")
	return nil
}

// openUpgradeEventLog opens UpgradeEventsDir/consensus-<operationId>.jsonl. Returns
// nil (logx-only sink) when the events dir is unset or the file cannot be opened.
func (um *UpgradeMonitor) openUpgradeEventLog(operationID string) *eventlog.EventLogger {
	if um.cfg.UpgradeEventsDir == "" || operationID == "" {
		return nil
	}
	logger, err := eventlog.NewOperation(um.cfg.UpgradeEventsDir, operationID)
	if err != nil {
		logx.As().Warn().Err(err).
			Str("operation_id", operationID).
			Str("dir", um.cfg.UpgradeEventsDir).
			Msg("could not open execute event log — JSONL audit disabled for this operation")
		return nil
	}
	return logger
}
