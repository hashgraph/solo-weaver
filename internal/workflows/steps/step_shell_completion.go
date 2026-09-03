// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// Seams so tests can script the host without touching /usr/local/share.
var (
	shellCompletionNeedsWrite  = software.ShellCompletionNeedsWrite
	reconfigureShellCompletion = software.ReconfigureShellCompletion
	removeShellCompletion      = software.RemoveShellCompletion
)

// SetupShellCompletion writes the bash completion loaders for the CLI tools
// installed earlier in this workflow.
//
// It is a step rather than part of each tool's Configure() because Configure is
// skipped once a tool reports itself configured — which it does as soon as its
// /usr/local/bin symlink resolves. A re-install over an existing host would then
// never write the loaders.
//
// It cannot fail the workflow: tab completion is an operator convenience, and
// the startup migration writes whatever is missing on the next run.
func SetupShellCompletion() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("setup-shell-completion").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up shell completion")
			return ctx, nil
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			if rpt.Status == automa.StatusSkipped {
				notify.As().StepCompletion(ctx, stp, rpt,
					"Shell completion not set up (best-effort); "+
						"the startup migration retries on the next run")
				return
			}

			notify.As().StepCompletion(ctx, stp, rpt, "Shell completion set up")
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if !shellCompletionNeedsWrite() {
				return automa.SuccessReport(stp, automa.WithMetadata(map[string]string{
					AlreadyConfigured: "true",
				}))
			}

			if err := reconfigureShellCompletion(); err != nil {
				logx.As().Warn().Err(err).
					Msg("Could not write the shell completion loaders; continuing without them")

				return automa.SkippedReport(stp,
					automa.WithDetail("Shell completion loaders could not be written"))
			}

			stp.State().Local().Set(ConfiguredByThisStep, true)

			return automa.SuccessReport(stp, automa.WithMetadata(map[string]string{
				ConfiguredByThisStep: "true",
			}))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			var writtenByThisStep bool
			if v, ok := stp.State().Local().Bool(ConfiguredByThisStep); ok {
				writtenByThisStep = v
			}

			if !writtenByThisStep {
				return automa.SkippedReport(stp,
					automa.WithDetail("Shell completion loaders were not written by this step"))
			}

			// Best-effort, like the rest of the teardown path.
			if err := removeShellCompletion(); err != nil {
				logx.As().Warn().Err(err).
					Msg("Could not remove the shell completion loaders during rollback")
			}

			return automa.SuccessReport(stp)
		})
}
