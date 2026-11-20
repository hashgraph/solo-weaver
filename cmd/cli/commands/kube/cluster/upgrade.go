// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/automa-saga/errx"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/bll/clusterupgrade"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/ui/prompt"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

var (
	flagUpgradePlan            bool
	flagUpgradeTo              string
	flagUpgradeAll             bool
	flagUpgradeDryRun          bool
	flagUpgradeStopOnError     bool
	flagUpgradeRollbackOnError bool
	flagUpgradeContinueOnError bool

	upgradeCmd = &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade the Kubernetes cluster to the version this binary installs, one step at a time",
		Long: "Upgrade the Kubernetes cluster to the version this binary installs. Each run reads the live cluster, " +
			"plans the remaining steps (one Kubernetes or Cilium minor at a time, with the chart changes each step " +
			"needs) and applies the next one. --all applies every remaining step, re-planning after each. " +
			"--plan lists the steps and --dry-run checks the next one; neither changes anything or takes a lock. " +
			"A run holds the system and cluster state locks, so other commands that change the cluster are refused until it ends.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagUpgradeRollbackOnError || flagUpgradeContinueOnError {
				return errx.Decorate(
					errorx.RejectedOperation.New("kube cluster upgrade supports only --stop-on-error"),
					reasons.PreconditionNotMet,
					"A Kubernetes upgrade cannot be rolled back, and the next step must not run after one failed. "+
						"Run the command again without the flag.")
			}
			opts := clusterupgrade.Options{To: flagUpgradeTo}
			switch {
			case flagUpgradePlan:
				plan, err := planLive(cmd.Context(), opts)
				if err != nil {
					return decoratePlanError(err)
				}
				return printPlan(cmd, plan)
			case flagUpgradeDryRun:
				res, err := dryRunLive(cmd.Context(), opts)
				if err != nil {
					return decorateUnlessDecorated(err)
				}
				return printDryRun(cmd, res)
			}

			force, err := common.FlagForce().Value(cmd, args)
			if err != nil {
				return err
			}
			var confirm func([]clusterupgrade.Hop) (bool, error)
			if prompt.ShouldPrompt(force) {
				if common.OutputIsJSON() {
					return errx.Decorate(errorx.RejectedOperation.New("a run with --output json cannot ask for confirmation"),
						reasons.PreconditionNotMet, "Pass --force to apply without the question.")
				}
				confirm = confirmHops
			}
			res, err := applyLive(cmd.Context(), clusterupgrade.ApplyOptions{Options: opts, All: flagUpgradeAll, Confirm: confirm})
			printErr := printApplyResult(cmd, res)
			if err != nil {
				return decorateUnlessDecorated(err)
			}
			return printErr
		},
	}
)

func init() {
	upgradeCmd.Flags().BoolVar(&flagUpgradePlan, "plan", false, "List every remaining upgrade step and change nothing")
	upgradeCmd.Flags().BoolVar(&flagUpgradePlan, "list", false, "Same as --plan")
	upgradeCmd.Flags().StringVar(&flagUpgradeTo, "to", "",
		"Stop at this Kubernetes minor (1.35) or its latest patch (1.35.10); defaults to the version this binary installs")
	upgradeCmd.Flags().BoolVar(&flagUpgradeAll, "all", false, "Apply every remaining step, re-planning after each, instead of only the next one")
	upgradeCmd.Flags().BoolVar(&flagUpgradeDryRun, "dry-run", false, "Show the next step and its pre-step checks, and change nothing")
	common.FlagStopOnError().SetVarP(upgradeCmd, &flagUpgradeStopOnError, false)
	// Accepted so the error can say why; an upgrade cannot roll back or go on.
	common.FlagRollbackOnError().SetVarPHidden(upgradeCmd, &flagUpgradeRollbackOnError, false)
	common.FlagContinueOnError().SetVarPHidden(upgradeCmd, &flagUpgradeContinueOnError, false)
	upgradeCmd.MarkFlagsMutuallyExclusive("plan", "dry-run", "all")
	upgradeCmd.MarkFlagsMutuallyExclusive("list", "dry-run", "all")
	common.GateExperimental(upgradeCmd, "cluster-upgrade")
}

// liveSources reads the host's state directory and live cluster.
func liveSources() (clusterupgrade.LiveSources, error) {
	sm, err := state.NewStateManager()
	if err != nil {
		return clusterupgrade.LiveSources{}, errorx.IllegalState.Wrap(err, "failed to create state manager")
	}
	checkers, err := reality.NewCheckers(sm)
	if err != nil {
		return clusterupgrade.LiveSources{}, err
	}
	return clusterupgrade.LiveSources{
		State:   sm,
		Cluster: checkers.Cluster,
		Machine: checkers.Machine,
		Managed: reality.ManagedReleases,
		Catalog: software.LoadInfrastructureCatalog,
		Probe:   checkers.Probe,
	}, nil
}

// Seams so tests can plan and apply without a host or a cluster.
var (
	planLive = func(ctx context.Context, opts clusterupgrade.Options) (clusterupgrade.Plan, error) {
		src, err := liveSources()
		if err != nil {
			return clusterupgrade.Plan{}, err
		}
		return clusterupgrade.PlanLive(ctx, src, opts)
	}
	dryRunLive = func(ctx context.Context, opts clusterupgrade.Options) (clusterupgrade.DryRunResult, error) {
		src, err := liveSources()
		if err != nil {
			return clusterupgrade.DryRunResult{}, err
		}
		return clusterupgrade.DryRun(ctx, src, opts)
	}
	applyLive = func(ctx context.Context, opts clusterupgrade.ApplyOptions) (clusterupgrade.ApplyResult, error) {
		src, err := liveSources()
		if err != nil {
			return failedResult(err), err
		}
		// Held for the whole run: other commands that change the cluster are
		// refused, and the plan cannot change between the read and the hop.
		locks, err := state.AcquireHeldLocks(filepath.Dir(src.State.State().StateFile),
			[]state.ComponentID{state.ComponentSystem, state.ComponentCluster}, 0)
		if err != nil {
			return failedResult(err), err
		}
		defer locks.Release()
		return clusterupgrade.Apply(ctx, src, locks, common.RunWorkflow, opts)
	}
)

// failedResult is the result of a run that failed before it read the plan.
func failedResult(err error) clusterupgrade.ApplyResult {
	return clusterupgrade.ApplyResult{Outcome: clusterupgrade.OutcomeFailed, Applied: []clusterupgrade.Hop{},
		Remaining: []clusterupgrade.Hop{}, Cause: doctor.OperatorMessage(err)}
}

// confirmHops asks the operator to confirm the hops a run will apply. Leaving
// the question with Ctrl-C is a no.
func confirmHops(hops []clusterupgrade.Hop) (bool, error) {
	desc := ""
	for i, h := range hops {
		desc += fmt.Sprintf("%d. %s\n", i+1, h.Describe())
	}
	desc += "Workloads on the node restart while Kubernetes, Cilium and the charts are upgraded."
	ok, err := prompt.RunConfirm(fmt.Sprintf("Apply %d cluster upgrade %s?", len(hops), plural(len(hops), "step", "steps")), desc, false)
	if errors.Is(err, prompt.ErrAborted) {
		return false, nil
	}
	return ok, err
}

// printPlan writes the plan to stdout, as text or as JSON with --output json.
// A plan with blockers is printed, then returned as an error so the command
// exits non-zero.
func printPlan(cmd *cobra.Command, plan clusterupgrade.Plan) error {
	out := cmd.OutOrStdout()
	if common.OutputIsJSON() {
		data, err := json.MarshalIndent(plan, "", "  ")
		if err != nil {
			return errorx.InternalError.Wrap(err, "failed to render the plan as JSON")
		}
		fmt.Fprintln(out, string(data))
	} else {
		fmt.Fprint(out, renderPlan(plan))
	}

	if len(plan.Blockers) > 0 {
		return errx.Decorate(
			errorx.RejectedOperation.New("the cluster upgrade cannot be planned: %d blocker(s)", len(plan.Blockers)),
			reasons.PreconditionNotMet,
			"Fix each blocker listed above, then run the plan again.")
	}
	return nil
}

// printDryRun writes the next step and its checks. Blockers or a failed check
// make the command exit non-zero.
func printDryRun(cmd *cobra.Command, res clusterupgrade.DryRunResult) error {
	if res.Next == nil {
		return printPlan(cmd, res.Plan)
	}
	out := cmd.OutOrStdout()
	if common.OutputIsJSON() {
		data, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return errorx.InternalError.Wrap(err, "failed to render the dry run as JSON")
		}
		fmt.Fprintln(out, string(data))
	} else {
		fmt.Fprint(out, renderDryRun(res))
	}
	if failed := res.Checks.Failed(); len(failed) > 0 {
		return errx.Decorate(
			errorx.RejectedOperation.New("the next upgrade step cannot run: %d check(s) failed", len(failed)),
			reasons.PreconditionNotMet,
			"Fix each failed check listed above, then run the dry run again.")
	}
	return nil
}

// upgradeResultType is the "type" of the JSON object an apply run prints last.
const upgradeResultType = "upgrade"

// printApplyResult writes what the run did. It runs whatever the outcome, so a
// failed run still says which steps were applied.
func printApplyResult(cmd *cobra.Command, res clusterupgrade.ApplyResult) error {
	if res.Outcome == "" {
		return nil
	}
	out := cmd.OutOrStdout()
	if common.OutputIsJSON() {
		data, err := json.Marshal(struct {
			Type string `json:"type"`
			clusterupgrade.ApplyResult
		}{upgradeResultType, res})
		if err != nil {
			return errorx.InternalError.Wrap(err, "failed to render the upgrade result as JSON")
		}
		fmt.Fprintln(out, string(data))
		return nil
	}
	fmt.Fprint(out, renderApplyResult(res))
	return nil
}

// decorateUnlessDecorated keeps the reason and hints an error already carries,
// such as a failed step's, and decorates a plain plan error.
func decorateUnlessDecorated(err error) error {
	if _, ok := errx.ReasonOf(err); ok {
		return err
	}
	return decoratePlanError(err)
}

func decoratePlanError(err error) error {
	switch {
	case errorx.IsOfType(err, errorx.IllegalArgument):
		return errx.Decorate(err, reasons.InvalidArgument,
			"Pass --to as a Kubernetes minor such as 1.35, or that minor's latest patch.")
	case errorx.IsOfType(err, errorx.IllegalFormat):
		return errx.Decorate(err, reasons.Internal)
	default:
		return errx.Decorate(err, reasons.StateOperationFailed,
			"Run the command with sudo, check that 'kubectl get nodes' works on this host, and see the log for the cause.")
	}
}
