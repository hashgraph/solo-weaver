// SPDX-License-Identifier: Apache-2.0

package node

import (
	"fmt"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install a Hedera consensus node",
	Long:  "Deploy a consensus node by creating the required solo-operator CRs (Orbit, config CRs, ConsensusCapsule)",
	RunE: func(cmd *cobra.Command, args []string) error {
		scope := fmt.Sprintf("node%d", flagNodeId)
		if flagGrpcTlsSecret == "" {
			flagGrpcTlsSecret = scope + "-grpc-tls-keys"
		}
		if flagSigningSecret == "" {
			flagSigningSecret = scope + "-gossip-keys"
		}
		if flagHapiAppSecret == "" {
			flagHapiAppSecret = scope + "-hapi-app-keys"
		}

		inputs := models.ConsensusNodeInputs{
			Namespace:            flagNamespace,
			OrbitName:            flagOrbitName,
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
		}

		logx.As().Info().
			Int64("nodeId", inputs.NodeId).
			Str("orbit", inputs.OrbitName).
			Str("namespace", inputs.Namespace).
			Msg("Installing consensus node")

		wb := automa.NewWorkflowBuilder().WithId("consensus-node-install").
			Steps(
				steps.InstallSoloOperator(flagUpgradeOperator),
				steps.PrecheckOperatorCRDs(steps.ConsensusNodeCRDs...),
				steps.PrecheckOperatorRunning(),
				steps.PrecheckOperatorVersion(),
				steps.PrecheckConsensusSecrets(inputs),
				steps.EnsureOrbit(inputs),
				steps.EnsureConfigCRs(inputs),
				steps.CreateConsensusCapsule(inputs),
			)

		if err := common.RunWorkflowBuilder(cmd.Context(), wb); err != nil {
			return err
		}

		logx.As().Info().
			Int64("nodeId", inputs.NodeId).
			Msg("Successfully installed consensus node")

		return nil
	},
}
