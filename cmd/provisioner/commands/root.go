package commands

import (
	"fmt"
	"github.com/spf13/cobra"
	"golang.hedera.com/solo-provisioner/internal/config"
	"golang.hedera.com/solo-provisioner/pkg/logx"
)

var (
	// Used for flags.
	flagConfig string
	flagPoll   bool // exit after execution

	rootCmd = &cobra.Command{
		Use:   "provisioner",
		Short: "A user friendly tool to provision Hedera network components",
		Long:  "Solo Provisioner - A user friendly tool to provision Hedera network components",
	}
)

// Execute executes the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVarP(&flagConfig, "config", "c", "", "config file path")
	rootCmd.PersistentFlags().BoolVarP(&flagPoll, "poll", "", true, "poll for marker files")

	// make flags mandatory
	_ = rootCmd.MarkPersistentFlagRequired("config")
	//_ = rootCmd.MarkPersistentFlagRequired("host-id")
	//_ = rootCmd.MarkPersistentFlagRequired("start-date")
	//_ = rootCmd.MarkPersistentFlagRequired("end-date")
}

func initConfig() {
	var err error
	err = config.Initialize(flagConfig)
	if err != nil {
		fmt.Println("failed to initialize config")
		cobra.CheckErr(err)
	}

	err = logx.Initialize(config.Get().Log)
	if err != nil {
		fmt.Println(err)
		cobra.CheckErr(err)
	}
}
