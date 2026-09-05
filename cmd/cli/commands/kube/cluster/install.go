// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"strings"

	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/workflows"
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

		wb := workflows.WithWorkflowExecutionMode(workflows.InstallClusterWorkflow(skipHardwareChecks, mr, profile, sizingNodeType), opts)
		if err := common.RunWorkflowBuilder(cmd.Context(), wb); err != nil {
			return err
		}

		logx.As().Info().Msg("Successfully installed Kubernetes Cluster")
		return nil
	},
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
