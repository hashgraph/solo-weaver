// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/alloy"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/block"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/kube"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/teleport"
	"github.com/hashgraph/solo-weaver/internal/blocknode"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/version"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

// examples:
// ./weaver block node check --profile=local
// ./weaver block node setup --config ./config.yaml --profile=mainnet
// ./weaver consensus node check --profile=testnet
// ./weaver consensus node setup --config ./config.yaml --profile=perfnet

// rootCmd represents the base command when called without any subcommands
var (
	// Used for flags.
	flagConfig             string
	flagVersion            bool
	flagOutputFormat       string
	flagSkipHardwareChecks bool
	flagForce              bool
	flagProxy              bool
	flagLogLevel           string

	rootCmd = &cobra.Command{
		Use:   "solo-provisioner",
		Short: "A user friendly tool to provision Hedera network components",
		Long:  "Solo Provisioner - A user friendly tool to provision Hedera network components",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return common.RunGlobalChecks(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagVersion {
				return version.Print(cmd, flagOutputFormat)
			}

			return cmd.Help()
		},
	}
)

// Register state migrations at startup
func init() {
	common.FlagLogLevel().SetVarP(rootCmd, &flagLogLevel, false)
	common.FlagForce().SetVarP(rootCmd, &flagForce, false)
	common.FlagConfig().SetVarP(rootCmd, &flagConfig, false)

	// support '--version', '-v' to show version information
	common.FlagVersion().SetVarP(rootCmd, &flagVersion, false)
	common.FlagOutputFormat().SetVarP(rootCmd, &flagOutputFormat, false)

	// Hardware checks override flag - hidden to discourage casual use
	common.FlagSkipHardwareChecks().SetVarP(rootCmd, &flagSkipHardwareChecks, false)
	_ = rootCmd.PersistentFlags().MarkHidden(common.FlagSkipHardwareChecks().Name)

	// disable command sorting to keep the order of commands as added
	cobra.EnableCommandSorting = false

	// disable global checks for install command since we need to install it first without any checks
	common.SkipGlobalChecks(selfInstallCmd)

	// add subcommands
	rootCmd.AddCommand(selfInstallCmd)
	rootCmd.AddCommand(selfUninstallCmd)
	rootCmd.AddCommand(kube.GetCmd())
	rootCmd.AddCommand(block.GetCmd())
	rootCmd.AddCommand(teleport.GetCmd())
	rootCmd.AddCommand(alloy.GetCmd())
	rootCmd.AddCommand(version.Cmd())

	if common.DetectShortNameCollisions(rootCmd) {
		logx.As().Warn().Msg("flag short name collisions detected among commands; consider using unique short names " +
			"to avoid confusion when using flags with multiple commands")
	}

	RegisterMigrations()

}

func RegisterMigrations() {
	// Register all migrations at startup in a sequence that reflects the order of changes in the codebase.
	// This ensures that when a user upgrades from an older version, all necessary migrations will be applied in the
	// correct order to update their state to be compatible with the new version.
	migration.Register(state.MigrationComponent, state.NewUnifiedStateMigration())
	migration.Register(state.MigrationComponent, state.NewHelmReleaseSchemaV2Migration())
	migration.Register(blocknode.ComponentBlockNode, blocknode.NewVerificationStorageMigration())
	migration.Register(blocknode.ComponentBlockNode, blocknode.NewPluginsStorageMigration())
	migration.Register(workflows.MigrationComponent, workflows.NewLegacyBinaryMigration())
}

// Execute executes the root command.
func Execute(ctx context.Context) error {
	if ctx == nil {
		return errorx.IllegalArgument.New("context is required")
	}

	// execute the root command
	_, err := rootCmd.ExecuteContextC(ctx)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to execute command")
	}

	return nil
}
