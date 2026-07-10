// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/internal/network/policy"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// NftWeaverPersistStepId is the step ID for NftWeaverPersist.
const NftWeaverPersistStepId = "nft-weaver-persist"

// NftWeaverPersist re-renders /etc/solo-provisioner/network-weaver.nft from
// the policy registry and restarts the shared solo-provisioner-network-nft.service
// oneshot so the kernel loads both network-host.nft and network-weaver.nft via
// systemd — bringing the service's RemainAfterExit state in sync with the full
// static plane installed by `network policy create`.
//
// Set elements are deliberately NOT persisted; the daemon's poll loop
// rehydrates them within one ~5 s cycle after reboot.
func NftWeaverPersist() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(NftWeaverPersistStepId).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Persisting nftables weaver rules for reboot")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to persist nftables weaver rules")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "nftables weaver rules persisted")
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			empty, err := policy.IsRegistryEmpty(policy.RegistryDir)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.Decorate(err, "failed to read policy registry %s", policy.RegistryDir).
						WithProperty(models.ErrPropertyResolution, []string{
							"Check the policy registry: ls " + policy.RegistryDir,
							"Verify the directory is readable: ls -la " + policy.RegistryDir,
						})))
			}
			// Reconcile the persisted file to the registry. On an empty registry
			// this removes any stale network-weaver.nft so the boot oneshot never
			// replays a harmful/old inet weaver table (an empty chain renders as
			// policy drop with no accept for new connections).
			if err := policy.RenderWeaverNft(policy.RegistryDir, policy.WeaverNftPath, ""); err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.Decorate(err, "failed to render %s", policy.WeaverNftPath).
						WithProperty(models.ErrPropertyResolution, []string{
							"Ensure `network policy create` has been run for each BN category before install",
							"Check the policy registry: ls " + policy.RegistryDir,
							"Inspect the nft file for syntax errors: nft -c -f " + policy.WeaverNftPath,
						})))
			}
			if empty {
				return automa.SkippedReport(stp,
					automa.WithDetail("policy registry is empty; ensured no network-weaver.nft is persisted — weaver rules are written after `network policy create` runs"))
			}
			if err := firewall.EnsureNetworkNftUnit(ctx); err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.Decorate(err, "failed to install %s", policy.NetworkNftService).
						WithProperty(models.ErrPropertyResolution, []string{
							"Check systemd is running: systemctl status",
							"Verify the unit path is writable: ls -la /usr/lib/systemd/system/",
						})))
			}
			if err := policy.RestartNetworkNftService(ctx); err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.Decorate(err, "failed to restart %s", policy.NetworkNftService).
						WithProperty(models.ErrPropertyResolution, []string{
							"Check service status: systemctl status " + policy.NetworkNftService,
							"Verify nft is installed: which nft || apt-get install -y nftables",
							"Inspect the nft file: nft -c -f " + policy.WeaverNftPath,
						})))
			}
			return automa.SuccessReport(stp)
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			// Rollback is a no-op: the rendered file and restarted service are
			// idempotent artifacts. Removing network-weaver.nft on rollback would
			// leave the live inet weaver table present but un-replayed on next
			// reboot — no worse than before this step ran. Teardown is owned by
			// the `block node uninstall` workflow.
			return automa.SkippedReport(stp,
				automa.WithDetail("nft-weaver rollback is a no-op; teardown is handled by block node uninstall"))
		})
}
