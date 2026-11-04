// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"context"
	"strings"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install a Kubernetes Cluster",
	Long:  "Run safety checks, setup a K8s cluster",
	RunE: func(cmd *cobra.Command, args []string) error {
		// --node-type names the single workload intended for this cluster and, with
		// --profile, selects which hardware floor the host is validated against
		// (block and consensus have different requirements). Cluster install itself
		// is workload-agnostic and installs no operator/CRDs — the solo-operator is
		// installed separately by `kube operator install` (it needs a private-registry
		// pull secret first). The two flags have different scopes: --node-type may
		// stand alone (validate only), but --profile requires --node-type — you cannot
		// size a floor without knowing the workload.
		nodeType := strings.TrimSpace(flagNodeType)
		nodeTypeSet := cmd.Flags().Changed(common.FlagNodeType().Name)

		if nodeType != "" && !sanity.Contains(nodeType, models.AllNodeTypes()) {
			return errx.Decorate(
				errorx.IllegalArgument.New("invalid --node-type %q", nodeType),
				reasons.InvalidArgument,
				"Use one of: "+strings.Join(models.AllNodeTypes(), ", "))
		}

		profile, err := common.FlagProfile().Value(cmd, args)
		if err != nil {
			return errorx.IllegalArgument.Wrap(err, "failed to get %s flag", common.FlagProfile().Name)
		}

		// Sizing: --profile needs an explicit --node-type to select the floor.
		sizingNodeType := ""
		if profile != "" {
			if !nodeTypeSet || nodeType == "" {
				return errx.Decorate(
					errorx.IllegalArgument.New("--profile requires --node-type for 'kube cluster install'"),
					reasons.InvalidArgument,
					"Add --node-type to say which workload to size the host for (e.g. --profile local --node-type consensus)")
			}
			sizingNodeType = nodeType
			logx.As().Info().Msgf("--profile=%s: validating the host against a %s-node hardware floor", profile, sizingNodeType)
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
			Msg("Installing Kubernetes Cluster")

		skipHardwareChecks, err := common.FlagSkipHardwareChecks().Value(cmd, args)
		if err != nil {
			return errorx.IllegalArgument.Wrap(err, "failed to get %s flag", common.FlagSkipHardwareChecks().Name)
		}

		sr, err := common.Setup()
		if err != nil {
			return err
		}

		mr, ok := sr.Runtime.MachineRuntime.(*rsl.MachineRuntimeResolver)
		if !ok {
			return errorx.IllegalArgument.New("expected MachineRuntime to be *rsl.MachineRuntimeResolver but got %T", sr.Runtime.MachineRuntime)
		}

		// Setting up Kubernetes installs host binaries, so this command owns the
		// system record: it holds that lock for the whole run, then records what
		// the host now has. Only the system file is written, so only the machine
		// runtime is refreshed.
		owned := []state.ComponentID{state.ComponentSystem}
		release, err := state.AcquireComponentLocks(sr.Runtime.StateDir(), owned, 0)
		if err != nil {
			return err
		}
		defer release()

		wb := workflows.WithWorkflowExecutionMode(workflows.InstallClusterWorkflow(skipHardwareChecks, mr, profile, sizingNodeType), opts)
		intent := models.Intent{Action: models.ActionInstall, Target: models.TargetCluster}
		inputs := map[string]any{"profile": profile, "nodeType": nodeType, "skipHardwareChecks": skipHardwareChecks}
		// Record inside the run, before its report is saved and summarised, so
		// a warning about recording it reaches both. A failed install returns
		// its own error from the report.
		var recordErr error
		if err := common.RunWorkflow(cmd.Context(), func() (*automa.Report, error) {
			wf, err := wb.Build()
			if err != nil {
				return nil, err
			}
			report := wf.Execute(cmd.Context())
			entry := steps.WorkflowEntry(cmd.Context(), intent, inputs, report)
			recordErr = recordClusterInstall(cmd.Context(), sr.Runtime, entry, steps.FailureOf(report), owned)
			return report, nil
		}); err != nil {
			return err
		}
		if recordErr != nil {
			return recordErr
		}

		logx.As().Info().Msg("Successfully installed Kubernetes Cluster")
		return nil
	},
}

// clusterRecorder is what recordClusterInstall needs of the runtime;
// *rsl.RuntimeResolver satisfies it.
type clusterRecorder interface {
	RefreshMachine(ctx context.Context, force bool) (state.State, error)
	Commit(currentState state.State, entry state.ActionHistory, ids ...state.ComponentID) error
	Record(entry state.ActionHistory, cause error) error
}

// recordClusterInstall records the install once its workflow ran. A failed
// install (failure is the workflow's error) leaves the system record as it
// was, records only that it failed, and returns failure; otherwise the live
// state's owned files are written with the entry.
func recordClusterInstall(ctx context.Context, rt clusterRecorder, entry state.ActionHistory, failure error, owned []state.ComponentID) error {
	record := func(err error) error {
		_ = rt.Record(entry, err) // with a cause, a lost entry is a state warning
		return err
	}
	if failure != nil {
		return record(failure)
	}
	live, err := rt.RefreshMachine(ctx, true)
	if err != nil {
		entry.NotPersisted = true
		return record(errorx.IllegalState.Wrap(err, "failed to refresh state after cluster install"))
	}
	if err := rt.Commit(live, entry, owned...); err != nil {
		if errorx.IsOfType(err, fsx.FileSyncError) {
			return err // the system record landed; rsl says what to check
		}
		return errorx.IllegalState.Wrap(err, "failed to persist the system record")
	}
	return nil
}

func init() {
	// --node-type declares the workload for hardware sizing (with --profile) and
	// validation. It does NOT install the operator — see `kube operator install`.
	common.FlagNodeType().SetVarP(installCmd, &flagNodeType, false)
	common.FlagStopOnError().SetVarP(installCmd, &flagStopOnError, false)
	common.FlagRollbackOnError().SetVarP(installCmd, &flagRollbackOnError, false)
	common.FlagContinueOnError().SetVarP(installCmd, &flagContinueOnError, false)
	installCmd.MarkFlagsMutuallyExclusive(
		common.FlagStopOnError().Name,
		common.FlagContinueOnError().Name,
		common.FlagRollbackOnError().Name,
	)
}
