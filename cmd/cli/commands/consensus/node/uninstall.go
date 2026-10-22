// SPDX-License-Identifier: Apache-2.0

package node

import (
	"fmt"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	cnbll "github.com/hashgraph/solo-weaver/internal/bll/consensus"
	"github.com/hashgraph/solo-weaver/internal/ui/prompt"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall a Hedera consensus node",
	Long: "Delete a consensus node's ConsensusCapsule and config CRs, and the Orbit when no other node uses it. " +
		"Secrets and volume data are kept. Idempotent: a node that is already gone is a success.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateUninstallFlags(cmd.Flags().Changed("node-id"), flagNamespace, flagNodeId); err != nil {
			return err
		}

		force, err := common.FlagForce().Value(cmd, args)
		if err != nil {
			return errx.Decorate(
				errorx.IllegalArgument.Wrap(err, "failed to get %s flag", common.FlagForce().Name),
				reasons.Internal)
		}

		// Deleting the capsule stops the node; confirm unless --force or non-interactive.
		if prompt.ShouldPrompt(force) {
			ok, err := prompt.RunConfirm(
				fmt.Sprintf("Uninstall consensus node %d from namespace %s?", flagNodeId, flagNamespace),
				"This deletes the node's ConsensusCapsule and config CRs, which stops the node. The Orbit is deleted "+
					"when no other node uses it. Secrets and volume data are kept.",
				false)
			if err != nil {
				return err
			}
			if !ok {
				return errorx.IllegalState.New("aborted: consensus node not uninstalled")
			}
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

		intent := models.Intent{
			Action: models.ActionUninstall,
			Target: models.TargetConsensusNode,
		}
		inputs := consensusUninstallInputs(force)

		logx.As().Info().
			Int64("nodeId", inputs.Custom.NodeId).
			Str("namespace", inputs.Custom.Namespace).
			Msg("Uninstalling consensus node")

		ac, err := handler.ForAction(intent.Action)
		if err != nil {
			return err
		}

		if err := common.RunWorkflow(cmd.Context(), func() (*automa.Report, error) {
			return ac.HandleIntent(cmd.Context(), intent, inputs)
		}); err != nil {
			return err
		}

		logx.As().Info().
			Int64("nodeId", inputs.Custom.NodeId).
			Msg("Successfully uninstalled consensus node")
		return nil
	},
}

// validateUninstallFlags rejects a missing --node-id (0 is a valid ID, so the default
// cannot mean "not set"), a negative ID, and an empty --namespace.
func validateUninstallFlags(nodeIdSet bool, namespace string, nodeId int64) error {
	if !nodeIdSet {
		return errx.Decorate(
			errorx.IllegalArgument.New("--node-id is required"),
			reasons.InvalidArgument,
			"Pass the ID of the node to uninstall, e.g. --node-id 0",
			fmt.Sprintf("List the nodes: kubectl -n %s get consensuscapsules", namespace))
	}
	if nodeId < 0 {
		return errx.Decorate(
			errorx.IllegalArgument.New("--node-id must not be negative, got %d", nodeId),
			reasons.InvalidArgument,
			"Pass a node ID of 0 or greater")
	}
	return common.ValidateNamespaceFlag(namespace)
}

// consensusUninstallInputs builds the uninstall inputs. Profile is left empty on
// purpose: a non-empty one would overwrite the machine profile in state.
func consensusUninstallInputs(force bool) models.UserInputs[models.ConsensusNodeInputs] {
	return models.UserInputs[models.ConsensusNodeInputs]{
		Common: models.CommonInputs{
			NodeType:         models.NodeTypeConsensus,
			Force:            force,
			ExecutionOptions: *workflows.DefaultWorkflowExecutionOptions(),
		},
		Custom: models.ConsensusNodeInputs{
			Namespace: flagNamespace,
			NodeId:    flagNodeId,
			// Unused by uninstall, but ConsensusNodeInputs.Validate requires them.
			AccountId: flagAccountId,
			Weight:    flagWeight,
		},
	}
}
