// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/ui/prompt"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall a Kubernetes Cluster",
	Long:  "Teardown the K8s cluster, stop services, remove bind mounts, and cleanup configuration files while preserving downloads cache",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Tearing down the cluster is destructive and irreversible — confirm first,
		// unless --force (or a non-interactive/no-TTY session, via ShouldPrompt).
		force, err := common.FlagForce().Value(cmd, args)
		if err != nil {
			return err
		}
		if prompt.ShouldPrompt(force) {
			ok, err := prompt.RunConfirm(
				"Uninstall the Kubernetes cluster?",
				"This tears down the cluster (kubeadm reset), stops services, removes bind mounts, and cleans up "+
					"configuration — destroying all workloads and cluster state on this host. The downloads cache is preserved.",
				false)
			if err != nil {
				return err
			}
			if !ok {
				return errorx.IllegalState.New("aborted: Kubernetes cluster not uninstalled")
			}
		}

		execMode, err := common.GetExecutionMode(flagContinueOnError, flagStopOnError, flagRollbackOnError)
		if err != nil {
			return errorx.Decorate(err, "failed to determine execution mode")
		}

		opts := workflows.DefaultWorkflowExecutionOptions()
		opts.ExecutionMode = execMode

		logx.As().Debug().
			Strs("args", args).
			Any("opts", opts).
			Msg("Uninstalling Kubernetes Cluster")

		// The teardown deletes every state file but the action history
		// (CleanupWeaverFiles), so it holds every state lock while it runs.
		// Cleanup deletes the .lock files too, while they are held: a command
		// that starts after that step gets fresh locks while the teardown
		// finishes. That is safe because nothing writes state after cleanup;
		// only this command's history entry is appended. Such a command's own
		// write, landing before that entry, would be credited to it: RecordAround
		// compares every state file after the teardown.
		dir := models.Paths().StateDir
		release, err := lockAllState(dir)
		if err != nil {
			return err
		}
		defer release()

		wf, err := workflows.WithWorkflowExecutionMode(workflows.UninstallClusterWorkflow(), opts).Build()
		if err != nil {
			return err
		}
		entry := state.ActionHistory{Intent: models.Intent{Action: models.ActionUninstall, Target: models.TargetCluster}}
		if err := state.RecordAround(dir, entry, func() (state.Outcome, error) {
			var report *automa.Report
			err := common.RunWorkflow(cmd.Context(), func() (*automa.Report, error) {
				report = wf.Execute(cmd.Context())
				return report, nil
			})
			return steps.OutcomeOf(report), err
		}); err != nil {
			return uninstallRecordError(dir, err)
		}

		logx.As().Info().Msg("Successfully uninstalled Kubernetes Cluster")
		return nil
	},
}

// uninstallRecordError says what a successful uninstall's lost or unsynced
// entry means for the operator; the uninstall itself already happened, so no
// hint suggests re-running it. Any other error is returned as is.
func uninstallRecordError(dir string, err error) error {
	switch {
	case errorx.IsOfType(err, state.EntryNotAppendedError):
		return errx.Decorate(
			errorx.Decorate(err, "the cluster was uninstalled, but its action history entry was not appended"),
			reasons.StateOperationFailed,
			"Check that "+state.ActionHistoryFilePath(dir)+" is writable.")
	case errorx.IsOfType(err, fsx.FileSyncError):
		return errx.Decorate(
			errorx.Decorate(err, "the cluster was uninstalled and its action history entry appended, but not synced to disk"),
			reasons.StateOperationFailed,
			"Check the disk holding "+dir+".")
	}
	return err
}

// lockAllState takes, failing fast, every lock that guards a state file: each
// component's, provisioner.yaml's, and the startup migrations'. The returned
// release unwinds them all.
func lockAllState(dir string) (release func(), err error) {
	releaseComponents, err := state.AcquireComponentLocks(dir, state.AllComponentIDs, 0)
	if err != nil {
		return nil, err
	}
	releaseProvisioner, err := state.AcquireProvisionerLock(dir, 0)
	if err != nil {
		releaseComponents()
		return nil, err
	}
	releaseMigrations, err := state.AcquireStartupMigrationsLock(dir, 0)
	if err != nil {
		releaseProvisioner()
		releaseComponents()
		return nil, err
	}
	return func() {
		releaseMigrations()
		releaseProvisioner()
		releaseComponents()
	}, nil
}

func init() {
	common.FlagStopOnError().SetVarP(uninstallCmd, &flagStopOnError, false)
	common.FlagRollbackOnError().SetVarP(uninstallCmd, &flagRollbackOnError, false)
	common.FlagContinueOnError().SetVarP(uninstallCmd, &flagContinueOnError, false)
	uninstallCmd.MarkFlagsMutuallyExclusive(
		common.FlagStopOnError().Name,
		common.FlagContinueOnError().Name,
		common.FlagRollbackOnError().Name,
	)
}
