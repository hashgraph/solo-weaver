// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/ui/prompt"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
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

		// The teardown deletes every state file (CleanupWeaverFiles), so it
		// holds every state lock while it runs. Cleanup keeps the action history
		// and the .lock files: a command started meanwhile waits on the same
		// locks instead of writing a state file RecordAround would credit to
		// this uninstall.
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
		entry := state.NewEntry(cmd.Context(), models.Intent{Action: models.ActionUninstall, Target: models.TargetCluster}, nil)
		// Record inside the run, before its report is saved and summarised, so
		// a warning about recording it reaches both. RecordAround returns the
		// workflow's failure, which the report already carries.
		if err := common.RunWorkflow(cmd.Context(), func() (*automa.Report, error) {
			var report *automa.Report
			_ = state.RecordAround(dir, entry, func() (state.Outcome, error) {
				report = wf.Execute(cmd.Context())
				return steps.OutcomeOf(report), steps.FailureOf(report)
			})
			return report, nil
		}); err != nil {
			return err
		}

		logx.As().Info().Msg("Successfully uninstalled Kubernetes Cluster")
		return nil
	},
}

// uninstallStartupLockWait lets a startup pass that is waiting on API timeouts
// during a deferral finish first. A var so tests can shorten it.
var uninstallStartupLockWait = 30 * time.Second

// lockAllState takes every lock that guards a state file: each component's,
// provisioner.yaml's, and the startup migrations'. It waits up to
// uninstallStartupLockWait for a running startup pass first, holding nothing
// else meanwhile, then takes the rest failing fast. The returned release
// unwinds them all.
func lockAllState(dir string) (release func(), err error) {
	releaseMigrations, err := state.AcquireStartupMigrationsLock(dir, uninstallStartupLockWait)
	if err != nil {
		return nil, err
	}
	releaseComponents, err := state.AcquireComponentLocks(dir, state.AllComponentIDs, 0)
	if err != nil {
		releaseMigrations()
		return nil, err
	}
	releaseProvisioner, err := state.AcquireProvisionerLock(dir, 0)
	if err != nil {
		releaseComponents()
		releaseMigrations()
		return nil, err
	}
	return func() {
		releaseProvisioner()
		releaseComponents()
		releaseMigrations()
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
