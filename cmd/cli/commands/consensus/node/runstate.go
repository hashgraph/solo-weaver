// SPDX-License-Identifier: Apache-2.0

package node

import (
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	cnbll "github.com/hashgraph/solo-weaver/internal/bll/consensus"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

const defaultRunStateReadyTimeout = 5 * time.Minute

var (
	flagStartReadyTimeout time.Duration
	flagStopReadyTimeout  time.Duration

	startCmd = &cobra.Command{
		Use:   "start",
		Short: "Start the consensus process of an installed node",
		Long: "Set the node's ConsensusCapsule spec.desiredRunState to Running; the solo-operator starts the " +
			"consensus process in the running pod. Works for any start policy. The network genesis must exist. " +
			"Idempotent: a node that already runs stays as it is.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunState(cmd, models.ActionStart, flagStartReadyTimeout)
		},
	}

	stopCmd = &cobra.Command{
		Use:   "stop",
		Short: "Stop the consensus process of an installed node",
		Long: "Set the node's ConsensusCapsule spec.desiredRunState to Stopped; the solo-operator stops the " +
			"consensus process. The pod and its sidecars stay up. After a pod restart the operator stops the process " +
			"again, until 'consensus node start'. Works for any start policy. Idempotent: a stopped node stays as it is.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunState(cmd, models.ActionStop, flagStopReadyTimeout)
		},
	}
)

func init() {
	startCmd.Flags().DurationVar(&flagStartReadyTimeout, "ready-timeout", defaultRunStateReadyTimeout,
		"How long to wait for the node to reach phase Running or Active (0 disables the wait)")
	stopCmd.Flags().DurationVar(&flagStopReadyTimeout, "ready-timeout", defaultRunStateReadyTimeout,
		"How long to wait for the operator to stop the consensus process (0 disables the wait)")
}

// runRunState runs the start or stop intent for --node-id in --namespace.
func runRunState(cmd *cobra.Command, action models.ActionType, timeout time.Duration) error {
	if err := validateNodeSelectorFlags(cmd.Flags().Changed("node-id"), flagNamespace, flagNodeId, string(action)); err != nil {
		return err
	}
	if timeout < 0 {
		return errx.Decorate(
			errorx.IllegalArgument.New("--ready-timeout must not be negative, got %s", timeout),
			reasons.InvalidArgument,
			"Pass a duration such as 5m, or 0 to skip the wait")
	}

	sr, err := common.Setup()
	if err != nil {
		return err
	}

	handler, err := cnbll.NewHandlerFactory(sr.Runtime)
	if err != nil {
		return errx.Decorate(
			errorx.IllegalState.Wrap(err, "failed to initialise consensus-node intent handler"),
			reasons.Internal)
	}

	intent := models.Intent{Action: action, Target: models.TargetConsensusNode}
	inputs := consensusRunStateInputs(timeout)

	ac, err := handler.ForAction(intent.Action)
	if err != nil {
		return err
	}

	logx.As().Info().Int64("nodeId", flagNodeId).Str("namespace", flagNamespace).Str("action", string(action)).
		Msg("Changing consensus node run state")

	return common.RunWorkflow(cmd.Context(), func() (*automa.Report, error) {
		return ac.HandleIntent(cmd.Context(), intent, inputs)
	})
}

// consensusRunStateInputs builds the start/stop inputs. Profile stays empty, as
// for uninstall, so the flush does not overwrite the machine profile.
func consensusRunStateInputs(timeout time.Duration) models.UserInputs[models.ConsensusNodeInputs] {
	return models.UserInputs[models.ConsensusNodeInputs]{
		Common: models.CommonInputs{
			NodeType:         models.NodeTypeConsensus,
			ExecutionOptions: *workflows.DefaultWorkflowExecutionOptions(),
		},
		Custom: models.ConsensusNodeInputs{
			Namespace:    flagNamespace,
			NodeId:       flagNodeId,
			ReadyTimeout: timeout,
		},
	}
}
