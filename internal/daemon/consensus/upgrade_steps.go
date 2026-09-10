// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"context"
	"fmt"
	"time"

	"github.com/automa-saga/automa"
	cn "github.com/hashgraph/solo-weaver/internal/consensus"
	"github.com/joomcode/errorx"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

// This file holds the execute-phase steps as standalone constructors — each takes
// its id, timeout, and the deps it needs, and returns an *automa.StepBuilder — so
// they compose into any workflow and a follow-up fills a single step without
// touching the orchestration. The bodies are TODO stubs; timedStep already gives
// each step its own deadline.
//
// Every step must be idempotent: the daemon retries and may restart mid-operation,
// so re-running a step must not repeat side effects (create-if-absent config CRs,
// verify-then-skip files, etc.).

// patchTimeout bounds a single status subresource patch.
const patchTimeout = 30 * time.Second

// timedStep runs fn under a context scoped to timeout, so no step can block
// indefinitely and leave the execution slot stuck.
func timedStep(id string, timeout time.Duration, fn func(context.Context) error) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(id).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			stepCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			if err := fn(stepCtx); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			return automa.StepSuccessReport(stp.Id())
		})
}

// stepExternalFiles verifies the download:prepare files are present and hashed and
// downloads the download:freeze files.
//
// TODO: apply the external-files.yaml policy (required entry failure halts, optional
// entry is skipped); verify-hash-then-skip for idempotency.
func stepExternalFiles(id string, timeout time.Duration) *automa.StepBuilder {
	return timedStep(id, timeout, func(context.Context) error { return nil })
}

// stepInfraVersionsPlacement atomically places infrastructure-versions.yaml onto
// the host.
//
// TODO: implement (skip when already placed with matching content).
func stepInfraVersionsPlacement(id string, timeout time.Duration) *automa.StepBuilder {
	return timedStep(id, timeout, func(context.Context) error { return nil })
}

// stepRuntimeSafetyGate refuses to proceed when the installed host runtime is below
// the declared minimum version.
//
// TODO: implement the installed-vs-declared min-version gate.
func stepRuntimeSafetyGate(id string, timeout time.Duration) *automa.StepBuilder {
	return timedStep(id, timeout, func(context.Context) error { return nil })
}

// stepInfraUpgradeDetect writes the durable PendingInfraUpgrade phase before any
// infra-mutating work when an infra upgrade is required (per HIP-1496: the daemon
// owns this phase so it survives a cluster teardown, during which the operator does
// not exist), then performs the upgrade. Basic CN upgrades need no infra upgrade, so
// the workflow proceeds straight to the config-CR steps.
func stepInfraUpgradeDetect(id string, timeout time.Duration, client dynamic.Interface, namespace, crName string, sink *eventSink) *automa.StepBuilder {
	return timedStep(id, timeout, func(ctx context.Context) error {
		required, err := infraUpgradeRequired(ctx)
		if err != nil {
			return err
		}
		if !required {
			return nil
		}
		sink.info(ReasonPendingInfraUpgrade, "writing PendingInfraUpgrade checkpoint before infra upgrade")
		if err := patchExecutePhase(ctx, client, namespace, crName, cn.PhasePendingInfraUpgrade); err != nil {
			return err
		}
		// TODO: run the infra upgrade (cluster teardown/reinstall, host tooling,
		// daemon self-upgrade), then resume on the PendingInfraUpgrade checkpoint.
		return nil
	})
}

// infraUpgradeRequired reports whether the declared infrastructure requires a host
// infra upgrade.
//
// TODO: compare infrastructure-versions.yaml against the installed host runtime.
func infraUpgradeRequired(ctx context.Context) (bool, error) {
	return false, ctx.Err()
}

// stepCreateConfigCRs scans the upgrade package and creates the per-operation
// ConsensusConfig CRs idempotently (AlreadyExists is success). Mirrors
// UC.deployConfigCRs's create half; the shared deployer records the created refs
// for the wait step.
func stepCreateConfigCRs(id string, timeout time.Duration, d *configCRDeployer) *automa.StepBuilder {
	return timedStep(id, timeout, d.create)
}

// stepWaitConfigReconcile waits for every ConsensusConfig CR the create step
// recorded to reach Valid=True; a CR that goes Valid=False is a terminal (fatal)
// failure. Mirrors UC.waitForConfigCRsValid.
func stepWaitConfigReconcile(id string, timeout time.Duration, d *configCRDeployer) *automa.StepBuilder {
	return timedStep(id, timeout, d.waitValid)
}

// patchExecutePhase durably writes phase to the CR's status.phase subresource. Per
// HIP-1496 the daemon writes only PendingInfraUpgrade and PendingNodeUpgrade; the
// operator owns the rest, so anything else is an internal invariant breach.
func patchExecutePhase(ctx context.Context, client dynamic.Interface, namespace, crName string, phase cn.Phase) error {
	if !phase.IsDaemonWritable() {
		return errorx.AssertionFailed.New("daemon refused to write non-daemon-writable phase %q to %s", phase, crName)
	}
	patch := []byte(fmt.Sprintf(`{"status":{"phase":%q}}`, phase))

	patchCtx, cancel := context.WithTimeout(ctx, patchTimeout)
	defer cancel()

	_, err := client.Resource(networkUpgradeExecuteGVR).Namespace(namespace).
		Patch(patchCtx, crName, types.MergePatchType, patch, metav1.PatchOptions{}, "status")
	if err != nil {
		return ErrK8sAPI.Wrap(err, "patch %s status.phase to %s", crName, phase)
	}
	return nil
}
