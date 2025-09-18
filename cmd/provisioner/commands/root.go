package commands

import (
	"context"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
	"golang.hedera.com/solo-provisioner/internal/config"
	"golang.hedera.com/solo-provisioner/internal/doctor"
	"golang.hedera.com/solo-provisioner/internal/version"
	"golang.hedera.com/solo-provisioner/pkg/logx"
)

// examples:
// ./provisioner system preflight --node-type block-node | consensus-node | --type all | os | cpu | memory | disk | network
// ./provisioner system benchmark --node-type block-node --type all | disk | cpu | memory --node-type block-node --output ./benchmark.yaml
// ./provisioner system setup --node-type block-node
// ./provisioner system upgrade --manifest ./upgrade-manifests
// ./provisioner system diagnose --output ./diagnostics --send

// ./provisioner solo-operator deploy
// ./provisioner solo-operator upgrade --manifest ./upgrade-manifest/solo-operator.yaml

// ./provisioner consensus-node deploy --manifest ./manifests/consensus-node.yaml

// ./provisioner block-node deploy --config ./config.yaml
// ./provisioner block-node upgrade --manifest ./upgrade-manifests/block-node.yaml

// ./provisioner mirror-node deploy --config ./config.yaml
// ./provisioner relay deploy --config ./config.yaml
// ./provisioner dashboard deploy --config ./config.yaml

// rootCmd represents the base command when called without any subcommands
var (
	// Used for flags.
	flagConfig string

	rootCmd = &cobra.Command{
		Use:   "provisioner",
		Short: "A user friendly tool to provision Hedera network components",
		Long:  "Solo Provisioner - A user friendly tool to provision Hedera network components",
	}

	systemCmd = &cobra.Command{
		Use:   "system",
		Short: "Commands to manage and configure the system for Hedera network components",
		Long:  "Commands to manage and configure the system for Hedera network components",
	}

	blockNodeCmd = &cobra.Command{
		Use:   "block-node",
		Short: "Commands to manage and configure block nodes",
		Long:  "Commands to manage and configure block nodes",
	}
)

// Execute executes the root command.
func Execute(ctx context.Context) error {
	if ctx == nil {
		return errorx.IllegalArgument.New("context is required")
	}

	cobra.OnInitialize(func() {
		initConfig(ctx)
	})

	rootCmd.PersistentFlags().StringVarP(&flagConfig, "config", "c", "config.yaml", "config file path")

	// make flags mandatory
	//_ = rootCmd.MarkPersistentFlagRequired("config")

	// system command
	systemCmd.AddCommand(preflightCmd)
	systemCmd.AddCommand(setupCmd)

	// block-node command
	blockNodeCmd.AddCommand(blockNodeDeploy)

	rootCmd.AddCommand(systemCmd)
	rootCmd.AddCommand(blockNodeCmd)

	_, err := rootCmd.ExecuteContextC(ctx)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to execute command")
	}

	return nil
}

func initConfig(ctx context.Context) {
	var err error
	err = config.Initialize(flagConfig)
	if err != nil {
		doctor.CheckErr(ctx, err)
	}

	logConfig := config.Get().Log
	err = logx.WithConfig(&logConfig, nil)
	if err != nil {
		doctor.CheckErr(ctx, err)
	}

	logx.WithContext(ctx, map[string]string{
		"commit":  version.Commit(),
		"version": version.Number(),
	}).Debug().Msg("Initialized configuration")
}
