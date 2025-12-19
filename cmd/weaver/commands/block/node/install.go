// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:     "install",
	Aliases: []string{"setup"}, // deprecated, will be removed soon
	Short:   "Install a Hedera Block Node",
	Long:    "Run safety checks, setup a K8s cluster and install a Hedera Block Node",
	RunE: func(cmd *cobra.Command, args []string) error {
		flagProfile, err := common.FlagProfile.Value(cmd, args)
		if err != nil {
			return errorx.IllegalArgument.Wrap(err, "failed to get profile flag")
		}

		if flagProfile == "" {
			return errorx.IllegalArgument.New("profile flag is required")
		}

		// Apply configuration overrides from flags
		applyConfigOverrides()

		// Validate the configuration after applying overrides
		// This catches invalid storage paths and other configuration issues early,
		// before the workflow starts and cluster creation begins
		if err := config.Get().Validate(); err != nil {
			return err
		}

		// Validate the values file path if provided
		// This is the primary security validation point for user-supplied file paths.
		var validatedValuesFile string
		if flagValuesFile != "" {
			validatedValuesFile, err = sanity.ValidateInputFile(flagValuesFile)
			if err != nil {
				return err
			}
		}

		logx.As().Debug().
			Strs("args", args).
			Str("nodeType", nodeType).
			Str("profile", flagProfile).
			Str("valuesFile", validatedValuesFile).
			Msg("Installing Hedera Block Node")

		common.RunWorkflow(cmd.Context(), workflows.NewBlockNodeInstallWorkflow(flagProfile, validatedValuesFile, nil))

		logx.As().Info().Msg("Successfully installed Hedera Block Node")
		return nil
	},
}

func init() {
	common.FlagValuesFile.SetVarP(installCmd, &flagValuesFile, false)
}

// applyConfigOverrides applies flag values to override the configuration.
// This allows flags to take precedence over config file values.
func applyConfigOverrides() {
	overrides := config.BlockNodeConfig{
		Namespace: flagNamespace,
		Release:   flagReleaseName,
		Chart:     flagChartRepo,
		Version:   flagChartVersion,
		Storage: config.BlockNodeStorage{
			BasePath:    flagBasePath,
			ArchivePath: flagArchivePath,
			LivePath:    flagLivePath,
			LogPath:     flagLogPath,
			LiveSize:    flagLiveSize,
			ArchiveSize: flagArchiveSize,
			LogSize:     flagLogSize,
		},
	}
	config.OverrideBlockNodeConfig(overrides)
}
