// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"os"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	pkgos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/joomcode/errorx"
)

// NftServiceTeardownStepId is the step ID for NftServiceTeardown.
const NftServiceTeardownStepId = "nft-service-teardown"

// NftServiceTeardown disables and removes the shared
// solo-provisioner-network-nft.service unit when neither the host-firewall
// artifact (network-weaver-host-firewall.nft) nor the workload-policy artifact
// (network-weaver-workload-policy.nft) are present — i.e. both planes have
// been torn down and nothing is left for the service to replay at boot.
//
// If the host-firewall file still exists (HostNftPath), the service is needed
// to replay it; the step skips so the firewall survives reboot until
// `kube cluster uninstall` removes it.
//
// It is wired into `block node uninstall` after NetworkPolicyDeleteAll (which
// removes the weaver-workload-policy.nft artifact when the last policy is
// deleted) and before the shape teardown steps. The step is idempotent: it
// skips gracefully when the unit file is already absent.
func NftServiceTeardown() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(NftServiceTeardownStepId).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Disabling network-nft boot service")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to disable network-nft boot service")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "network-nft boot service disabled")
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			// Keep the service if the host-firewall file is still present: kube cluster
			// uninstall owns that teardown and will disable the service there.
			// If os.Stat returns any error other than NotExist (e.g. permission denied,
			// I/O error) treat the file as potentially present and retain the service
			// rather than risk removing it while it is still needed.
			_, hostNftErr := os.Stat(firewall.HostNftPath)
			if hostNftErr == nil {
				logx.As().Info().Str("host_nft", firewall.HostNftPath).
					Msg("host-firewall file still present; keeping network-nft service for reboot replay")
				return automa.SkippedReport(stp,
					automa.WithDetail("host-firewall file still present — network-nft service retained for boot replay; kube cluster uninstall will disable it"))
			}
			if !os.IsNotExist(hostNftErr) {
				logx.As().Warn().Err(hostNftErr).Str("path", firewall.HostNftPath).
					Msg("cannot stat host-nft file; retaining network-nft service as a precaution")
				return automa.SkippedReport(stp,
					automa.WithDetail("cannot determine host-firewall file status; retaining network-nft service as a precaution"))
			}

			// Both nft planes are now gone. Tear down the service unit.
			if _, err := os.Stat(firewall.NetworkNftServiceUnitPath); os.IsNotExist(err) {
				logx.As().Info().Msg("network-nft service unit already absent; nothing to disable")
				return automa.SkippedReport(stp,
					automa.WithDetail("network-nft service unit already absent"))
			}

			// Stop the service (oneshot — may already be in exited state; log and continue on error).
			if running, _ := pkgos.IsServiceRunning(ctx, firewall.NetworkNftService); running {
				if err := pkgos.StopService(ctx, firewall.NetworkNftService); err != nil {
					logx.As().Warn().Err(err).Str("service", firewall.NetworkNftService).
						Msg("could not stop network-nft service; continuing teardown")
				}
			}

			if err := pkgos.DisableService(ctx, firewall.NetworkNftService); err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.Decorate(err, "failed to disable %s", firewall.NetworkNftService).
						WithProperty(models.ErrPropertyResolution, []string{
							"Disable manually: systemctl disable " + firewall.NetworkNftService,
							"Remove the unit file: rm " + firewall.NetworkNftServiceUnitPath,
						})))
			}

			if err := os.Remove(firewall.NetworkNftServiceUnitPath); err != nil && !os.IsNotExist(err) {
				return automa.FailureReport(stp, automa.WithError(
					errorx.ExternalError.Wrap(err, "failed to remove unit file %s", firewall.NetworkNftServiceUnitPath).
						WithProperty(models.ErrPropertyResolution, []string{
							"Remove the unit file manually: rm " + firewall.NetworkNftServiceUnitPath,
							"Then reload systemd: systemctl daemon-reload",
						})))
			}

			if err := pkgos.DaemonReload(ctx); err != nil {
				logx.As().Warn().Err(err).Msg("network-nft service disabled and unit removed, but daemon-reload failed")
			}

			logx.As().Info().Str("service", firewall.NetworkNftService).Msg("network-nft service disabled and unit removed")
			return automa.SuccessReport(stp)
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SkippedReport(stp,
				automa.WithDetail("nft-service teardown rollback is a no-op; re-enable via block node install"))
		})
}
