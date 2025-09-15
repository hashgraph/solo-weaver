package commands

import (
	"context"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
	"golang.hedera.com/solo-provisioner/internal/config"
	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/internal/doctor"
	"golang.hedera.com/solo-provisioner/internal/version"
	"golang.hedera.com/solo-provisioner/pkg/logx"
)

var (
	// Used for flags.
	flagConfig string

	rootCmd = &cobra.Command{
		Use:   "provisioner",
		Short: "A user friendly tool to provision Hedera network components",
		Long:  "Solo Provisioner - A user friendly tool to provision Hedera network components",
	}
)

// Execute executes the root command.
func Execute(ctx context.Context) error {
	if ctx == nil {
		return core.IllegalArgument.New("context is required")
	}

	cobra.OnInitialize(func() {
		initConfig(ctx)
	})

	rootCmd.PersistentFlags().StringVarP(&flagConfig, "config", "c", "", "config file path")

	// make flags mandatory
	_ = rootCmd.MarkPersistentFlagRequired("config")

	rootCmd.AddCommand(setupCmd)

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
