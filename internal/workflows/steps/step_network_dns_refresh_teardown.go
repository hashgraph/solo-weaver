// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// DNSRefreshServiceTeardownStepId is the step ID for DNSRefreshServiceTeardown.
const DNSRefreshServiceTeardownStepId = "network-dns-refresh-service-teardown"

// DNSRefreshServiceTeardown stops, disables and removes the
// solo-provisioner-network-dns-refresh.{timer,service} units, if either is
// present. Unlike NftServiceTeardown, which keeps the shared nft loader alive
// for as long as the host-firewall file is present, this unit has no reason to
// outlive the binary it re-invokes — a self-uninstalled host has no CLI left
// for it to call. Runs unconditionally: SyncDNSRefreshTimer(ctx, false) only
// touches the two unit files, never the config directory, so it has no
// ordering dependency on RemoveNetworkConfig and is idempotent when neither
// unit is installed.
func DNSRefreshServiceTeardown() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(DNSRefreshServiceTeardownStepId).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Removing DNS refresh boot service")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to remove DNS refresh boot service")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "DNS refresh boot service removed")
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if err := firewall.SyncDNSRefreshTimer(ctx, false); err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.Decorate(err, "failed to remove the DNS refresh boot service").
						WithProperty(models.ErrPropertyResolution, []string{
							"Disable manually: systemctl disable " + firewall.DNSRefreshTimer,
							"Remove the unit files: rm " + firewall.DNSRefreshTimerUnitPath + " " + firewall.DNSRefreshServiceUnitPath,
							"Then reload systemd: systemctl daemon-reload",
						})))
			}
			logx.As().Info().Str("unit", firewall.DNSRefreshTimer).
				Msg("DNS refresh boot service removed")
			return automa.SuccessReport(stp)
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SkippedReport(stp,
				automa.WithDetail("DNS refresh service teardown rollback is a no-op; the timer is reinstalled by the next mutation that holds a name"))
		})
}
