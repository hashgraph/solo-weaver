// SPDX-License-Identifier: Apache-2.0

package network

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

// genesisNetworkFileName is the pre-built genesis the operator writes verbatim
// into the genesis-config ConfigMap when present in the deployment package.
const genesisNetworkFileName = "genesis-network.json"

var genesisCmd = &cobra.Command{
	Use:   "genesis",
	Short: "Generate the network genesis for a fresh consensus network",
	Long: "Create the NetworkGenesis CR so the solo-operator produces genesis-network.json for the orbit. " +
		"When --deployment-package-dir is set the package's pre-built genesis-network.json is applied verbatim; " +
		"otherwise the operator discovers the roster from the ConsensusCapsules in the namespace. " +
		"Run this once per fresh network, after the consensus nodes are installed.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := common.Setup(); err != nil {
			return err
		}

		// A pre-built genesis is authoritative and skips the operator's cluster
		// roster discovery. An explicit --genesis-file overrides the deployment
		// package's genesis-network.json when both are provided.
		var genesisJSON string
		switch {
		case strings.TrimSpace(flagGenesisFile) != "":
			path := strings.TrimSpace(flagGenesisFile)
			data, err := os.ReadFile(path)
			if err != nil {
				return errx.Decorate(
					errorx.ExternalError.Wrap(err, "reading genesis override %s", path),
					reasons.FileMissing,
					fmt.Sprintf("Ensure %s exists and is readable, or omit --genesis-file to use the deployment package's %s", path, genesisNetworkFileName))
			}
			genesisJSON = string(data)
		case strings.TrimSpace(flagPkgDir) != "":
			path := filepath.Join(flagPkgDir, genesisNetworkFileName)
			data, err := os.ReadFile(path)
			if err != nil {
				return errx.Decorate(
					errorx.ExternalError.Wrap(err, "reading pre-built genesis %s", path),
					reasons.FileMissing,
					fmt.Sprintf("Ensure %s exists in the deployment package, or omit --deployment-package-dir to discover the roster from the cluster", genesisNetworkFileName))
			}
			genesisJSON = string(data)
		}

		orbit := flagNamespace
		stepList := []automa.Builder{
			steps.EnsureNetworkGenesis(flagNamespace, orbit, genesisJSON, steps.DefaultCapsuleKubeProvider),
		}
		if flagReadyTimeout > 0 {
			// After genesis unblocks the network, confirm the nodes actually come
			// up — otherwise a fresh install's readiness wait is skipped (no genesis
			// yet) and nothing verifies the nodes reach Running.
			stepList = append(stepList,
				steps.WaitNetworkGenesisReady(flagNamespace, steps.DefaultCapsuleKubeProvider, flagReadyTimeout),
				steps.WaitConsensusNetworkReady(flagNamespace, steps.DefaultCapsuleKubeProvider, flagReadyTimeout),
			)
		}
		wb := automa.NewWorkflowBuilder().WithId("consensus-network-genesis").Steps(stepList...)

		if err := common.RunWorkflow(cmd.Context(), func() (*automa.Report, error) {
			wf, err := wb.Build()
			if err != nil {
				return nil, err
			}
			return wf.Execute(cmd.Context()), nil
		}); err != nil {
			return err
		}

		logx.As().Info().Str("orbit", orbit).Msg("Network genesis generated")
		return nil
	},
}
