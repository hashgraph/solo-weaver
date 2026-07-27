// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/network/policy"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// NetworkPolicyDeleteAllStepId is the step ID for NetworkPolicyDeleteAll.
const NetworkPolicyDeleteAllStepId = "network-policy-delete-all"

// NetworkPolicyDeleteAll tears down the BN workload classification plane (the
// `inet weaver` table) by deleting every canonical BN policy. It is the disable
// counterpart to NetworkPolicyCreate, wired into `block node reconfigure` when
// the operator turns traffic shaping off on an already-provisioned block node.
//
// It walks canonicalBNPolicies and deletes only those that currently exist
// (Manager.Delete errors on a missing policy, so each is Exists-checked first),
// making the step idempotent. Each delete re-renders the live weaver chain
// without that policy; deleting the last remaining policy tears the whole `inet
// weaver` table down and removes the persisted network-weaver.nft, so no
// separate NftWeaverPersist is needed on the disable path.
func NetworkPolicyDeleteAll() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(NetworkPolicyDeleteAllStepId).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Removing network policies (inet weaver)")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to remove network policies")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Network policies removed")
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			mgr := newPolicyManager()
			var deleted int
			for _, c := range canonicalBNPolicies {
				exists, err := policy.Exists(c.name)
				if err != nil {
					return automa.FailureReport(stp, automa.WithError(
						errorx.Decorate(err, "failed to check whether network policy %q exists", c.name).
							WithProperty(models.ErrPropertyResolution, []string{
								"Inspect the policy registry: ls " + policy.RegistryDir,
							})))
				}
				if !exists {
					continue
				}
				if err := mgr.Delete(ctx, c.name); err != nil {
					return automa.FailureReport(stp, automa.WithError(
						errorx.Decorate(err, "failed to delete network policy %q", c.name).
							WithProperty(models.ErrPropertyResolution, []string{
								"Inspect the live weaver table: nft list table inet weaver",
								"Check the policy registry for leftover entries: ls " + policy.RegistryDir,
								"Re-run the reconfigure; policy deletion is idempotent (delete-if-present)",
							})))
				}
				deleted++
			}
			logx.As().Info().Int("deleted", deleted).Int("total", len(canonicalBNPolicies)).
				Msg("network policies removed")
			return automa.SuccessReport(stp)
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			// Rollback is a no-op: re-creating the BN policy plane requires the pod
			// CIDR and management allowlist that this teardown step does not carry.
			// Re-enabling traffic shaping is an explicit operator decision (block node
			// reconfigure --traffic-shaping-enabled), not a silent rollback action.
			return automa.SkippedReport(stp,
				automa.WithDetail("network policy delete rollback is a no-op; re-enable via block node reconfigure --traffic-shaping-enabled"))
		})
}
