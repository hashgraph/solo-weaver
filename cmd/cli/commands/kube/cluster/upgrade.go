// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/automa-saga/errx"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/bll/clusterupgrade"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

var (
	flagUpgradePlan bool
	flagUpgradeTo   string

	upgradeCmd = &cobra.Command{
		Use:   "upgrade",
		Short: "Plan the upgrade of the Kubernetes cluster to the version this binary installs",
		Long: "Plan the upgrade of the Kubernetes cluster. --plan reads the live cluster and lists every remaining " +
			"step to the target, one Kubernetes or Cilium minor at a time, with the chart changes each step needs. " +
			"It changes nothing and takes no lock. Applying the steps is not available yet.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !flagUpgradePlan {
				return errx.Decorate(
					errorx.RejectedOperation.New("applying upgrade steps is not available yet"),
					reasons.PreconditionNotMet,
					"Run 'solo-provisioner kube cluster upgrade --plan' to see the steps.")
			}
			plan, err := planLive(cmd.Context(), clusterupgrade.Options{To: flagUpgradeTo})
			if err != nil {
				return decoratePlanError(err)
			}
			return printPlan(cmd, plan)
		},
	}
)

func init() {
	upgradeCmd.Flags().BoolVar(&flagUpgradePlan, "plan", false, "List every remaining upgrade step and change nothing")
	upgradeCmd.Flags().BoolVar(&flagUpgradePlan, "list", false, "Same as --plan")
	upgradeCmd.Flags().StringVar(&flagUpgradeTo, "to", "",
		"Stop at this Kubernetes minor (1.35) or its latest patch (1.35.10); defaults to the version this binary installs")
	common.GateExperimental(upgradeCmd, "cluster-upgrade")
}

// planLive is a seam so tests can plan without a host or a cluster.
var planLive = func(ctx context.Context, opts clusterupgrade.Options) (clusterupgrade.Plan, error) {
	sm, err := state.NewStateManager()
	if err != nil {
		return clusterupgrade.Plan{}, errorx.IllegalState.Wrap(err, "failed to create state manager")
	}
	checkers, err := reality.NewCheckers(sm)
	if err != nil {
		return clusterupgrade.Plan{}, err
	}
	return clusterupgrade.PlanLive(ctx, clusterupgrade.LiveSources{
		State:   sm,
		Cluster: checkers.Cluster,
		Machine: checkers.Machine,
		Managed: reality.ManagedReleases,
		Catalog: software.LoadInfrastructureCatalog,
	}, opts)
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
