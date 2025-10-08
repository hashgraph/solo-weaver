package commands

import (
	"context"

	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
	"golang.hedera.com/solo-provisioner/internal/config"
	"golang.hedera.com/solo-provisioner/internal/doctor"
)

// examples:
// ./provisioner block node check
// ./provisioner consensus node check
// ./provisioner local node check

// Future commands (not yet implemented):
// ./provisioner block node setup --config ./config.yaml
// ./provisioner consensus node setup --manifest ./manifests/consensus-node.yaml
// ./provisioner local node setup

// NodeTypeConfig defines the configuration for a node type
type NodeTypeConfig struct {
	Name      string
	ParentCmd *cobra.Command
}

// rootCmd represents the base command when called without any subcommands
var (
	// Used for flags.
	flagConfig string

	rootCmd = &cobra.Command{
		Use:   "provisioner",
		Short: "A user friendly tool to provision Hedera network components",
		Long:  "Solo Provisioner - A user friendly tool to provision Hedera network components",
	}

	blockCmd = &cobra.Command{
		Use:   "block",
		Short: "Commands for block node type",
		Long:  "Commands for block node type",
	}

	consensusCmd = &cobra.Command{
		Use:   "consensus",
		Short: "Commands for consensus node type",
		Long:  "Commands for consensus node type",
	}

	localCmd = &cobra.Command{
		Use:   "local",
		Short: "Commands for local node type",
		Long:  "Commands for local node type",
	}
)

// nodeTypeConfigs defines all supported node types and their configuration
var nodeTypeConfigs = []NodeTypeConfig{
	{Name: "block", ParentCmd: blockCmd},
	{Name: "consensus", ParentCmd: consensusCmd},
	{Name: "local", ParentCmd: localCmd},
}

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
	//_ = rootCmd.MarkPersistentFlagRequired("cfg")

	// Setup node commands for each configured node type
	for _, cfg := range nodeTypeConfigs {
		// Create commands for this node type
		nodeCheckCmd := createNodeCheckCommand(cfg.Name)
		nodeSetupCmd := createNodeSetupCommand(cfg.Name)
		nodeSubCmd := createNodeSubcommand(cfg.Name)

		// Add check and setup commands to node subcommand
		nodeSubCmd.AddCommand(nodeCheckCmd)
		nodeSubCmd.AddCommand(nodeSetupCmd)

		// Add node subcommand to the parent command
		cfg.ParentCmd.AddCommand(nodeSubCmd)
	}

	// Add all node type commands to root
	for _, cfg := range nodeTypeConfigs {
		rootCmd.AddCommand(cfg.ParentCmd)
	}

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
	err = logx.Initialize(logConfig)
	if err != nil {
		doctor.CheckErr(ctx, err)
	}

	//logx.WithContext(ctx, map[string]string{
	//	"commit":  version.Commit(),
	//	"version": version.Number(),
	//}).Debug().Msg("Initialized configuration")
}

// createNodeSubcommand creates a "node" subcommand for a specific node type
func createNodeSubcommand(nodeType string) *cobra.Command {
	return &cobra.Command{
		Use:   "node",
		Short: "Commands to manage and configure " + nodeType + " nodes",
		Long:  "Commands to manage and configure " + nodeType + " nodes",
	}
}
