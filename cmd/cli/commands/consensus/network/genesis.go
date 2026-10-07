// SPDX-License-Identifier: Apache-2.0

package network

import (
	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	cnnbll "github.com/hashgraph/solo-weaver/internal/bll/consensus/network"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/spf13/cobra"
)

var genesisCmd = &cobra.Command{
	Use:   "genesis",
	Short: "Generate the network genesis for a fresh consensus network",
	Long: "Create the NetworkGenesis CR so the solo-operator produces genesis-network.json for the orbit. " +
		"When --deployment-package-dir is set the package's pre-built genesis-network.json is applied verbatim; " +
		"otherwise the operator discovers the roster from the ConsensusCapsules in the namespace. " +
		"Run this once per fresh network, after the consensus nodes are installed.",
	RunE: func(cmd *cobra.Command, args []string) error {
		sr, err := common.Setup()
		if err != nil {
			return err
		}

		// Reject an empty/whitespace --namespace before building or submitting any
		// CR — namespace doubles as spec.orbit here, and an empty value would
		// silently target the "default" namespace with an unset orbit.
		if err := common.ValidateNamespaceFlag(flagNamespace); err != nil {
			return err
		}

		intent := models.Intent{
			Action: models.ActionGenesis,
			Target: models.TargetConsensusNetwork,
		}

		inputs := models.UserInputs[models.ConsensusNetworkInputs]{
			Common: models.CommonInputs{
				ExecutionOptions: *workflows.DefaultWorkflowExecutionOptions(),
			},
			Custom: models.ConsensusNetworkInputs{
				Namespace:            flagNamespace,
				GenesisFile:          flagGenesisFile,
				DeploymentPackageDir: flagPkgDir,
				ReadyTimeout:         flagReadyTimeout,
			},
		}

		handler, err := cnnbll.NewHandlerFactory(sr.Runtime)
		if err != nil {
			return err
		}
		ac, err := handler.ForAction(intent.Action)
		if err != nil {
			return err
		}

		if err := common.RunWorkflow(cmd.Context(), func() (*automa.Report, error) {
			return ac.HandleIntent(cmd.Context(), intent, inputs)
		}); err != nil {
			return err
		}

		logx.As().Info().Str("orbit", flagNamespace).Msg("Network genesis generated")
		return nil
	},
}
