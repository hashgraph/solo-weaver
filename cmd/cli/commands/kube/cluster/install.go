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
		// the host now has. Only the system file is written, so flushing the
		// whole live state records just its Software and Hardware.
		owned := []state.ComponentID{state.ComponentSystem}
		release, err := state.AcquireComponentLocks(sr.Runtime.StateDir(), owned, 0)
		if err != nil {
			return err
		}
		defer release()

		wb := workflows.WithWorkflowExecutionMode(workflows.InstallClusterWorkflow(skipHardwareChecks, mr, profile, sizingNodeType), opts)
		// Keep the workflow's report: it says how the run ended, and whether it
		// recorded the provisioner version.
		var report *automa.Report
		runErr := common.RunWorkflow(cmd.Context(), func() (*automa.Report, error) {
			wf, err := wb.Build()
			if err != nil {
				return nil, err
			}
			report = wf.Execute(cmd.Context())
			return report, nil
		})
		if report == nil {
			// The workflow never started, so the host is unchanged.
			return runErr
		}

		entry := state.ActionHistory{
			Intent:  models.Intent{Action: models.ActionInstall, Target: models.TargetCluster},
			Inputs:  map[string]any{"profile": profile, "nodeType": nodeType, "skipHardwareChecks": skipHardwareChecks},
			Outcome: steps.OutcomeOf(report),
		}
		if steps.ProvisionerVersionPersisted(report) {
			entry.Components = []state.ChangedFile{state.ChangedProvisioner}
		}
		if steps.ProvisionerVersionFailed(report) {
			entry.NotPersisted = true
			entry.Error = "the provisioner version was not recorded; startup migrations may run again on the next invocation"
		}
		if err := recordClusterInstall(cmd.Context(), sr.Runtime, entry, runErr, owned); err != nil {
			return err
		}

		logx.As().Info().Msg("Successfully installed Kubernetes Cluster")
		return nil
	},
}

// clusterRecorder is what recordClusterInstall needs of the runtime;
// *rsl.RuntimeResolver satisfies it.
type clusterRecorder interface {
	Refresh(ctx context.Context, force bool) (state.State, error)
	Commit(currentState state.State, entry state.ActionHistory, ids ...state.ComponentID) error
	Record(entry state.ActionHistory, cause error) error
}

// recordClusterInstall records the install once its workflow ran. A failed
// install leaves the system record as it was, and records only that it
// failed; otherwise the live state's owned files are written with the entry.
func recordClusterInstall(ctx context.Context, rt clusterRecorder, entry state.ActionHistory, runErr error, owned []state.ComponentID) error {
	record := func(err error) error {
		_ = rt.Record(entry, err) // with a cause, a lost entry is a state warning
		return err
	}
	if runErr != nil {
		return record(runErr)
	}
	live, err := rt.Refresh(ctx, true)
	if err != nil {
		entry.NotPersisted = true
		return record(errorx.IllegalState.Wrap(err, "failed to refresh state after cluster install"))
	}
	if err := rt.Commit(live, entry, owned...); err != nil {
		if errorx.IsOfType(err, state.EntryNotAppendedError) || errorx.IsOfType(err, fsx.FileSyncError) {
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
