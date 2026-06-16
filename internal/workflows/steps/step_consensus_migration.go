// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/daemon/consensus"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// SoakStartStep sends POST /consensus_node/migration/soak/start to the daemon socket
// and surfaces TUI / non-interactive output via the notify layer.
func SoakStartStep(sockPath string, req consensus.SoakStartRequest) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("consensus-soak-start").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, fmt.Sprintf(
				"Starting consensus-node migration soak watcher (node_id=%s cutover_ts=%s)",
				req.NodeID, req.CutoverTimestamp.UTC().Format("2006-01-02T15:04:05Z")))
			return ctx, nil
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if _, err := SoakStart(sockPath, req); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.New("soak start failed: %v", err).
						WithProperty(models.ErrPropertyResolution, []string{
							fmt.Sprintf("Check soak status: solo-provisioner consensus migration soak status"),
							fmt.Sprintf("Check daemon journal: sudo journalctl -u %s -g SoakStartAccepted -n 20 --no-pager", daemonServiceName),
							fmt.Sprintf("Verify daemon is running: sudo systemctl status %s", daemonServiceName),
							"Verify no soak is already active — only one soak may run at a time",
						})))
			}
			return automa.StepSuccessReport(stp.Id())
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to start consensus-node migration soak watcher")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, fmt.Sprintf(
				"Consensus-node migration soak watcher started (node_id=%s)", req.NodeID))
		})
}

// SoakStopStep sends DELETE /consensus_node/migration/soak to the daemon socket
// and surfaces TUI / non-interactive output via the notify layer.
// When keepState is true the soak state file is preserved so the daemon resumes
// the soak on the next restart.
func SoakStopStep(sockPath string, keepState bool) *automa.StepBuilder {
	stateAction := "state deleted — daemon will NOT resume on next restart"
	if keepState {
		stateAction = "state preserved — daemon WILL resume on next restart"
	}
	return automa.NewStepBuilder().WithId("consensus-soak-stop").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, fmt.Sprintf(
				"Stopping consensus-node migration soak watcher (%s)", stateAction))
			return ctx, nil
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if err := SoakStop(sockPath, keepState); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.New("soak stop failed: %v", err).
						WithProperty(models.ErrPropertyResolution, []string{
							"Check whether a soak is active: solo-provisioner consensus migration soak status",
							fmt.Sprintf("Check daemon journal: sudo journalctl -u %s -g SoakStopped -n 20 --no-pager", daemonServiceName),
							fmt.Sprintf("Verify daemon is running: sudo systemctl status %s", daemonServiceName),
							"If the daemon is unresponsive, restart it: sudo systemctl restart " + daemonServiceName,
						})))
			}
			return automa.StepSuccessReport(stp.Id())
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to stop consensus-node migration soak watcher")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, fmt.Sprintf(
				"Consensus-node migration soak watcher stopped (%s)", stateAction))
		})
}
