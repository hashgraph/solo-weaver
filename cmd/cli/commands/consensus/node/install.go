// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	cnbll "github.com/hashgraph/solo-weaver/internal/bll/consensus"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install a Hedera consensus node",
	Long:  "Deploy a consensus node by creating the required solo-operator CRs (Orbit, config CRs, ConsensusCapsule)",
	RunE: func(cmd *cobra.Command, args []string) error {
		scope := models.ConsensusNodeScope(flagNodeId)
		if flagGrpcTlsSecret == "" {
			flagGrpcTlsSecret = scope + "-grpc-tls-keys"
		}
		if flagSigningSecret == "" {
			flagSigningSecret = scope + "-gossip-keys"
		}
		if flagHapiAppSecret == "" {
			flagHapiAppSecret = scope + "-hapi-app-keys"
		}

		sr, err := common.Setup()
		if err != nil {
			return err
		}

		handler, err := cnbll.NewHandlerFactory(sr.Runtime)
		if err != nil {
			return errorx.IllegalState.Wrap(err, "failed to initialise consensus-node intent handler")
		}

		force, err := common.FlagForce().Value(cmd, args)
		if err != nil {
			return errorx.IllegalArgument.Wrap(err, "failed to get %s flag", common.FlagForce().Name)
		}

		intent := models.Intent{
			Action: models.ActionInstall,
			Target: models.TargetConsensusNode,
		}

		inputs := models.UserInputs[models.ConsensusNodeInputs]{
			Common: models.CommonInputs{
				NodeType: models.NodeTypeConsensus,
				Force:    force,
			},
			Custom: models.ConsensusNodeInputs{
				Namespace:            flagNamespace,
				NodeId:               flagNodeId,
				AccountId:            flagAccountId,
				Weight:               flagWeight,
				LedgerId:             flagLedgerId,
				ChainId:              flagChainId,
				ConsensusImageRepo:   flagImageRepo,
				ConsensusImageTag:    flagImageTag,
				DeploymentPackageDir: flagDeploymentPkgDir,
				GrpcTlsSecret:        flagGrpcTlsSecret,
				SigningSecret:        flagSigningSecret,
				HapiAppSecret:        flagHapiAppSecret,
				UpgradeOperator:      flagUpgradeOperator,
			},
		}

		logx.As().Info().
			Int64("nodeId", inputs.Custom.NodeId).
			Str("namespace", inputs.Custom.Namespace).
			Msg("Installing consensus node")

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
			Msg("Successfully installed consensus node")

		return nil
	},
}
