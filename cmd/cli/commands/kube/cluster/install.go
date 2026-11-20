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

		// Setting up Kubernetes installs host binaries and the cluster charts,
		// so this command owns the system and cluster records: it holds their
		// locks for the whole run, then records what the host and cluster now
		// have.
		owned := []state.ComponentID{state.ComponentSystem, state.ComponentCluster}
		release, err := state.AcquireComponentLocks(sr.Runtime.StateDir(), owned, 0)
		if err != nil {
			return err
		}
		defer release()

		wb := workflows.WithWorkflowExecutionMode(workflows.InstallClusterWorkflow(skipHardwareChecks, mr, profile, sizingNodeType), opts)
		intent := models.Intent{Action: models.ActionInstall, Target: models.TargetCluster}
		inputs := map[string]any{"profile": profile, "nodeType": nodeType, "skipHardwareChecks": skipHardwareChecks}
		wb.Steps(steps.RecordState(recordClusterInstall(sr.Runtime, owned)))
		if err := common.RunWorkflow(cmd.Context(), installRun(cmd.Context(), wb, sr.Runtime, intent, inputs)); err != nil {
			return err
		}

		logx.As().Info().Msg("Successfully installed Kubernetes Cluster")
		return nil
	},
}

// installRun is the RunWorkflow function of installCmd. It appends the run's
// entry once the workflow has finished, before its report is saved and
// summarised, so a warning about the entry shows in the summary.
func installRun(ctx context.Context, wb automa.Builder, rt clusterRecorder, intent models.Intent, inputs any) func() (*automa.Report, error) {
	return func() (*automa.Report, error) {
		wf, err := wb.Build()
		if err != nil {
			return nil, err
		}
		report := wf.Execute(ctx)
		steps.RecordEntry(rt, steps.WorkflowEntry(ctx, intent, inputs, report), report)
		return report, nil
	}
}

// clusterRecorder is what installCmd needs of the runtime to record the
// install; *rsl.RuntimeResolver satisfies it.
type clusterRecorder interface {
	RefreshMachineAndCluster(ctx context.Context, force bool) (state.State, error)
	Persist(currentState state.State, ids ...state.ComponentID) ([]state.ChangedFile, error)
	Record(entry state.ActionHistory, cause error) error
}

// recordClusterInstall is the RecordState write of kube cluster install: what
// the host and cluster now have, in the owned records.
func recordClusterInstall(rt clusterRecorder, owned []state.ComponentID) func(ctx context.Context) ([]state.ChangedFile, error) {
	return func(ctx context.Context) ([]state.ChangedFile, error) {
		live, err := rt.RefreshMachineAndCluster(ctx, true)
		if err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to refresh state after cluster install")
		}
		return rt.Persist(live, owned...)
	}
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
