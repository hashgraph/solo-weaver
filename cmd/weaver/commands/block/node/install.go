// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:     "install",
	Aliases: []string{"setup"}, // deprecated, will be removed soon
	Short:   "Install a Hedera Block Node",
	Long:    "Run safety checks, setup a K8s cluster and install a Hedera Block Node",
	RunE: func(cmd *cobra.Command, args []string) error {
		err := initializeDependencies(cmd.Context())
		if err != nil {
			return err
		}

		inputs, err := prepareBlocknodeInputs(cmd, args)
		if err != nil {
			return err
		}

		intent := models.Intent{
			Action: models.ActionInstall,
			Target: models.TargetBlocknode,
		}

		logx.As().Info().
			Any("intent", intent).
			Any("inputs", inputs).
			Msg("Installing Hedera Block Node")

		report, err := bll.BlockNode().HandleIntent(intent, *inputs)
		if err != nil {
			return err
		}

		common.CheckWorkflowReport(cmd.Context(), report)

		logx.As().Info().Msg("Successfully installed Hedera Block Node")

		return nil
	},
}

func init() {
	initializeExecutionFlags(installCmd)
	common.FlagValuesFile.SetVarP(installCmd, &flagValuesFile, false)
}

// applyConfigOverrides applies flag values to override the configuration.
// This allows flags to take precedence over config file values.
func applyConfigOverrides() {
	overrides := models.BlockNodeConfig{
		Namespace: flagNamespace,
		Release:   flagReleaseName,
		Chart:     flagChartRepo,
		Version:   flagChartVersion,
		Storage: models.BlockNodeStorage{
			BasePath:         flagBasePath,
			ArchivePath:      flagArchivePath,
			LivePath:         flagLivePath,
			LogPath:          flagLogPath,
			VerificationPath: flagVerificationPath,
			PluginsPath:      flagPluginsPath,
			LiveSize:         flagLiveSize,
			ArchiveSize:      flagArchiveSize,
			LogSize:          flagLogSize,
			VerificationSize: flagVerificationSize,
			PluginsSize:      flagPluginsSize,
		},
	}
	config.OverrideBlockNodeConfig(overrides)
}
