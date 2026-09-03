// SPDX-License-Identifier: Apache-2.0

package node

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	cnbll "github.com/hashgraph/solo-weaver/internal/bll/consensus"
	"github.com/hashgraph/solo-weaver/internal/ui/prompt"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/hardware"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install a Hedera consensus node",
	Long:  "Deploy a consensus node by creating the required solo-operator CRs (Orbit, config CRs, ConsensusCapsule)",
	RunE: func(cmd *cobra.Command, args []string) error {
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

		force, err := common.FlagForce().Value(cmd, args)
		if err != nil {
			return errx.Decorate(
				errorx.IllegalArgument.Wrap(err, "failed to get %s flag", common.FlagForce().Name),
				reasons.Internal)
		}

		// Interactive prompts for any core-identity flags not supplied on the
		// command line. Skipped under --non-interactive / --force / no-TTY, and
		// per-flag when the operator already passed the flag.
		if err := promptForMissingFlags(cmd, force); err != nil {
			return err
		}

		// Derive per-node secret defaults from the (possibly prompted) node ID.
		// A consensus node needs exactly two secrets: gossip signing keys and the
		// gRPC TLS key/cert.
		scope := models.ConsensusNodeScope(flagNodeId)
		if flagGrpcTlsSecret == "" {
			flagGrpcTlsSecret = scope + "-grpc-tls-keys"
		}
		if flagSigningSecret == "" {
			flagSigningSecret = scope + "-gossip-keys"
		}

		// --profile sizes the host hardware floor when this install has to
		// bootstrap the cluster. It is required (interactive mode prompts for it
		// above; --non-interactive / --force must pass it explicitly), mirroring
		// block node install. When a cluster already exists it is unused, but we
		// require it uniformly for a consistent, predictable contract.
		if flagProfile == "" {
			return errx.Decorate(
				errorx.IllegalArgument.New("profile flag is required"),
				reasons.InvalidArgument,
				fmt.Sprintf("Pass --profile with one of: %v", models.SupportedProfiles()),
				"Interactive runs prompt for it; --non-interactive and --force require it explicitly")
		}
		if !hardware.IsValidProfile(flagProfile) {
			return errx.Decorate(
				errorx.IllegalArgument.New("unsupported profile: %q", flagProfile),
				reasons.InvalidArgument,
				fmt.Sprintf("Pass one of the supported profiles via --profile: %v", models.SupportedProfiles()))
		}

		skipHardwareChecks, err := common.FlagSkipHardwareChecks().Value(cmd, args)
		if err != nil {
			return errx.Decorate(
				errorx.IllegalArgument.Wrap(err, "failed to get %s flag", common.FlagSkipHardwareChecks().Name),
				reasons.Internal)
		}

		intent := models.Intent{
			Action: models.ActionInstall,
			Target: models.TargetConsensusNode,
		}

		inputs := models.UserInputs[models.ConsensusNodeInputs]{
			Common: models.CommonInputs{
				NodeType:         models.NodeTypeConsensus,
				Force:            force,
				ExecutionOptions: *workflows.DefaultWorkflowExecutionOptions(),
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
				ImagePullSecret:      flagImagePullSecret,
				Profile:              flagProfile,
				SkipHardwareChecks:   skipHardwareChecks,
				ContainerName:        flagContainerName,
				JavaHeapMin:          flagJavaHeapMin,
				JavaHeapMax:          flagJavaHeapMax,
				JavaOpts:             flagJavaOpts,
				CPULimit:             flagCPULimit,
				CPURequest:           flagCPURequest,
				MemoryLimit:          flagMemoryLimit,
				MemoryRequest:        flagMemoryRequest,
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

		// The node's ConsensusCapsule is created but does not start on its own: a
		// fresh network stays in genesis-init until the network genesis is generated
		// (and Manual-start nodes wait for `node start`). The status step's live hint
		// is transient in the TUI, so print the durable next steps here.
		printConsensusInstallNextSteps(cmd, flagNamespace, flagDeploymentPkgDir)

		return nil
	},
}

// printConsensusInstallNextSteps writes the post-install guidance to stdout after
// the workflow TUI has closed and restored the terminal, so it persists in the
// scrollback (unlike a transient step-detail line).
func printConsensusInstallNextSteps(cmd *cobra.Command, namespace, pkgDir string) {
	base := fmt.Sprintf("sudo solo-provisioner consensus network genesis --namespace %s", namespace)
	pkg := strings.TrimSpace(pkgDir)
	if pkg == "" {
		pkg = "<deployment-package-dir>"
	}

	cmd.Println()
	cmd.Println("Next steps:")
	cmd.Println("  1. Generate the network genesis so the node(s) can leave genesis-init and start.")
	cmd.Println("     Choose one:")
	cmd.Println("     a) Discovery (recommended for a fresh in-cluster network — operator writes cluster-DNS endpoints):")
	cmd.Printf("          %s\n", base)
	cmd.Println("     b) From a deployment package (mainnet/testnet/... — its genesis-network.json is applied verbatim):")
	cmd.Printf("          %s --deployment-package-dir %s\n", base, pkg)
	cmd.Println("     c) From a custom genesis file:")
	cmd.Printf("          %s --genesis-file <path/to/genesis-network.json>\n", base)
	cmd.Println("  2. Watch the node come up:")
	cmd.Printf("       kubectl -n %s get consensuscapsules\n", namespace)
	cmd.Println("     (A Manual start-policy node stays Stopped until 'consensus node start'.)")
}

// promptForMissingFlags presents an interactive wizard for the core consensus
// install flags the operator did not supply. It is a no-op under
// --non-interactive / --force / non-TTY (prompt.ShouldPrompt) and per-flag when
// the flag was already set on the command line, so scripted runs are unaffected.
func promptForMissingFlags(cmd *cobra.Command, force bool) error {
	if !prompt.ShouldPrompt(force) {
		return nil
	}

	cv := prompt.NewChosenValues()
	w := prompt.NewWizard()

	// node-id is an int64 flag; prompt into a string, then parse it back below.
	nodeIdStr := strconv.FormatInt(flagNodeId, 10)

	prompt.AddSelectPrompts(w, cmd, prompt.ConsensusNodeSelectPrompts(&flagProfile), cv)
	prompt.AddInputPrompts(w, cmd,
		prompt.ConsensusNodeInputPrompts(&flagNamespace, &nodeIdStr, &flagAccountId, &flagDeploymentPkgDir), cv)

	if err := w.Run(); err != nil {
		return err
	}

	// Parse the (possibly prompted) node ID back into the int64 flag var. The
	// prompt validator guarantees a non-negative integer; parse defensively so a
	// skipped prompt (unchanged string) round-trips cleanly.
	if n, perr := strconv.ParseInt(strings.TrimSpace(nodeIdStr), 10, 64); perr == nil {
		flagNodeId = n
	}

	// Without a deployment package, the image/ledger identity it would supply must
	// come from flags — so prompt for whatever the operator did not pass on the CLI
	// rather than dead-ending later with "image-repo/image-tag could not be resolved".
	if strings.TrimSpace(flagDeploymentPkgDir) == "" {
		w2 := prompt.NewWizard()
		prompt.AddInputPrompts(w2, cmd,
			prompt.ConsensusNodeIdentityInputPrompts(&flagImageRepo, &flagImageTag, &flagLedgerId, &flagChainId), cv)
		if err := w2.Run(); err != nil {
			return err
		}
	}

	cv.Print("Selected values:")
	return nil
}
