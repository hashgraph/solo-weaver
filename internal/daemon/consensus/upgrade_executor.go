// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"context"
	"fmt"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/daemonkit/eventlog"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	cn "github.com/hashgraph/solo-weaver/internal/consensus"
	"github.com/joomcode/errorx"
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
// execute-phase workflow for a single CR and reports the outcome per the HIP-1496
// provisioner failure contract. A future migrate flow gets its own executor.
type upgradeExecutor struct {
	client    dynamic.Interface
	namespace string
	crName    string
	sink      *eventSink

	// startTime is the CR's status.startTime (RFC3339) — the moment it entered
	// ReadyForProvisionerDaemon, and the durable anchor for the handoff deadline.
	// Empty when the operator has not stamped it yet.
	startTime string
	// deadline bounds retries across all watch re-deliveries; measured from
	// startTime. Exceeding it forces DaemonResult=False (reason DeadlineExceeded).
	deadline time.Duration

	// Config-CR inputs: the operationId (per-op CR names), the staged upgrade
	// package root, the node scope (node<N>), the raw node id (block-node file
	// selection), and the owning Orbit.
	operationID string
	upgradePath string
	scope       string
	nodeID      string
	orbit       string
}

// run executes the workflow and reports the outcome per HIP-1496 Provisioner
// Failure Semantics (docs/dev/execute-phase-handshake.md):
//
//   - success → reportSuccess (ConfigCRsApplied=True, DaemonResult=True, phase →
//     PendingNodeUpgrade); a patch failure there is transient and returns an error
//     so the handshake retries.
//   - fatal error (no errorx.Temporary trait) → DaemonResult=False with the error's
//     reason, no phase advance. MUST NOT retry (the operator marks the CR Failed).
//   - transient error within the deadline → left to retry via watch re-delivery
//     (return non-nil ⇒ the monitor does not mark the op complete); a WARN event is
//     emitted so it does not stall silently.
//   - transient error past the deadline → DaemonResult=False (DeadlineExceeded) so a
//     stuck operation terminates rather than hanging.
//
// A non-nil return leaves the op out of completedOpIDs (retryable); reportFailure is
// best-effort, so if its patch fails the op stays retryable and is reported again on
// the next delivery — preserving the termination guarantee.
func (x *upgradeExecutor) run(ctx context.Context) error {
	return x.reportOutcome(ctx, runExecuteWorkflow(ctx, x.sink, x.buildWorkflow()))
}

// reportOutcome applies the HIP-1496 failure contract to the workflow result and
// returns the value that gates completedOpIDs bookkeeping in the monitor: nil only
// on a clean success handshake, otherwise the workflow error (so the op stays
// retryable). See run for the branch semantics.
func (x *upgradeExecutor) reportOutcome(ctx context.Context, workflowErr error) error {
	if workflowErr == nil {
		return x.reportSuccess(ctx)
	}

	switch {
	case !errorx.IsTemporary(workflowErr):
		x.reportFailure(ctx, x.fatalReason(workflowErr), workflowErr.Error())
	case x.deadlineExceeded():
		x.reportFailure(ctx, cn.ReasonDaemonDeadlineExceeded,
			fmt.Sprintf("provisioner retry budget exceeded before handoff: %v", workflowErr))
	default:
		x.sink.warn(ReasonExecuteWorkflowRetrying,
			fmt.Sprintf("transient execute failure — retrying within handoff deadline: %v", workflowErr))
	}
	return workflowErr
}

// buildWorkflow assembles the execute-phase steps in their required order. Each body
// is a stub today (upgrade_steps.go); this ordering and the per-step timeouts are
// the contract. StopOnError halts at the first failing step. The terminal handshake
// (DaemonResult / ConfigCRsApplied conditions) is reportOutcome, not a step, because
// it reports the whole workflow's outcome.
func (x *upgradeExecutor) buildWorkflow() *automa.WorkflowBuilder {
	// One deployer instance is shared by the create and wait steps: create records
	// the CR refs, wait consumes them.
	cfgDeployer := &configCRDeployer{
		client:       x.client,
		namespace:    x.namespace,
		upgradePath:  x.upgradePath,
		scope:        x.scope,
		orbit:        x.orbit,
		nodeID:       x.nodeID,
		operationID:  x.operationID,
		pollInterval: configCRPollInterval,
	}
	return automa.NewWorkflowBuilder().
		WithId(executeWorkflowID).
		WithExecutionMode(automa.StopOnError).
		Steps(
			stepExternalFiles("external-files", stepTimeoutExternalFiles),
			stepInfraVersionsPlacement("infra-versions-placement", stepTimeoutInfraVersions),
			stepRuntimeSafetyGate("runtime-safety-gate", stepTimeoutSafetyGate),
			stepInfraUpgradeDetect("infra-upgrade-detect", stepTimeoutInfraDetect, x.client, x.namespace, x.crName, x.sink),
			stepCreateConfigCRs("create-config-crs", stepTimeoutCreateConfigCRs, cfgDeployer),
			stepWaitConfigReconcile("wait-config-reconcile", stepTimeoutWaitReconcile, cfgDeployer),
		)
}

// reportSuccess completes the HIP-1496 success handshake: it sets
// ConfigCRsApplied=True and DaemonResult=True, then advances the phase to
// PendingNodeUpgrade (the daemon owns that phase). Idempotent. A patch failure is
// transient and returned so the operation retries — the operator cannot advance
// without a complete handshake.
func (x *upgradeExecutor) reportSuccess(ctx context.Context) error {
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

// reportFailure emits the failed lifecycle event and writes DaemonResult=False
// (with reason + message) WITHOUT advancing the phase — the operator reads it and
// marks the CR Failed. Best-effort: a patch failure is logged, not returned, so the
// caller's non-nil return keeps the op retryable and it is reported again next
// delivery (termination guarantee). Called for fatal failures and for transient
// failures that outlast the handoff deadline.
func (x *upgradeExecutor) reportFailure(ctx context.Context, reason cn.ConditionReason, message string) {
	x.sink.error(ReasonExecuteWorkflowFailed, message)
	if err := setExecuteCondition(ctx, x.client, x.namespace, x.crName,
		cn.DaemonResultCondition, cn.ConditionFalse, reason, message); err != nil {
		logx.As().Error().Err(err).
			Str("cr_name", x.crName).
			Str("reason", string(reason)).
			Msg("failed to write DaemonResult=False — will report again on next delivery")
	}
}

// fatalReason resolves the DaemonResult reason for a fatal error: the errx reason
// the failing step attached, or the generic InfraUpgradeFailed fallback (mirroring
// the UC provisioner-proxy).
func (x *upgradeExecutor) fatalReason(err error) cn.ConditionReason {
	if r, ok := errx.ReasonOf(err); ok && r != "" {
		return cn.ConditionReason(r.String())
	}
	return cn.ReasonDaemonInfraUpgradeFailed
}

// deadlineExceeded reports whether the handoff budget, measured from status.startTime
// (when the CR entered ReadyForProvisionerDaemon), has elapsed. A missing or
// unparseable anchor is treated as NOT exceeded so the operation stays retryable
// until the operator stamps it — a missing anchor must not force a premature
// terminal failure.
func (x *upgradeExecutor) deadlineExceeded() bool {
	if x.startTime == "" || x.deadline <= 0 {
		return false
	}
	started, err := time.Parse(time.RFC3339, x.startTime)
	if err != nil {
		return false
	}
	return time.Since(started) > x.deadline
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

	startTime, _, _ := unstructured.NestedString(cr.Object, "status", "startTime")
	orbit, _, _ := unstructured.NestedString(cr.Object, "spec", "orbit")
	if orbit == "" {
		orbit = um.cfg.Namespace
	}
	x := &upgradeExecutor{
		client:      um.client,
		namespace:   um.cfg.Namespace,
		crName:      cr.GetName(),
		sink:        sink,
		startTime:   startTime,
		deadline:    um.cfg.handoffDeadline(),
		operationID: operationID,
		upgradePath: um.cfg.UpgradeDir,
		scope:       "node" + um.cfg.NodeID,
		nodeID:      um.cfg.NodeID,
		orbit:       orbit,
	}
	return x.run(ctx)
}

// runExecuteWorkflow runs wb, emitting the started/completed lifecycle events. It
// does NOT emit the failed event: whether a workflow error is a terminal failure or
// a transient retry is decided by run() (a transient retry emits a WARN, not a
// failure), so the failed event is emitted by reportFailure only on a terminal
// outcome. Split out so tests can drive the event contract with an injected workflow.
func runExecuteWorkflow(ctx context.Context, sink *eventSink, wb *automa.WorkflowBuilder) error {
	sink.info(ReasonExecuteWorkflowStarted, "execute-phase workflow started")
	report := automa.RunWorkflow(ctx, wb)
	if report.HasError() {
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
