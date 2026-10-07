// SPDX-License-Identifier: Apache-2.0

package node

import (
	"strings"

	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	blocknode "github.com/hashgraph/solo-weaver/internal/blocknode"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/hardware"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Runs safety checks to validate system readiness for Hedera Block node",
	Long:  "Runs safety checks to validate system readiness for deploying Hedera Block node",
	RunE: func(cmd *cobra.Command, args []string) error {
		flagProfile, err := common.FlagProfile().Value(cmd, args)
		if err != nil {
			return errorx.IllegalArgument.Wrap(err, "failed to get profile flag")
		}

		if flagProfile == "" {
			return errorx.IllegalArgument.New("profile flag is required")
		}

		// Validate profile early for better error messages
		if !hardware.IsValidProfile(flagProfile) {
			return errorx.IllegalArgument.New("unsupported profile: %q. Supported profiles: %v",
				flagProfile, models.SupportedProfiles())
		}

		// Set the profile in the global config so other components can access it
		config.SetProfile(flagProfile)

		// Resolve plugin options for hardware sizing.
		// --plugins overrides --plugin-preset when both are set (mirrors init.go precedence).
		opts := map[string]any{}
		if f := cmd.Flag("plugins"); f != nil && f.Changed {
			if err := models.ValidatePluginList(flagPlugins); err != nil {
				return errorx.IllegalArgument.Wrap(err, "invalid --plugins value")
			}
			opts["plugins"] = splitPlugins(flagPlugins)
			opts["preset"] = blocknode.PresetCustom
		} else if flagPluginPreset != "" {
			opts["preset"] = flagPluginPreset
		}

		deploySpec := hardware.DeploymentSpec{
			NodeType: strings.ToLower(nodeType),
			Profile:  strings.ToLower(flagProfile),
			Options:  opts,
		}

		logx.As().Debug().
			Strs("args", args).
			Str("nodeType", nodeType).
			Str("profile", flagProfile).
			Str("pluginPreset", flagPluginPreset).
			Msg("Running preflight checks for Hedera Block Node")

		effStorage, effChartVersion := storageForCheck()

		if err := common.RunWorkflowBuilder(cmd.Context(),
			workflows.NewBlockNodePreflightCheckWorkflow(deploySpec, effStorage, effChartVersion)); err != nil {
			return err
		}

		logx.As().Info().Msg("Node preflight checks completed successfully for block node")
		return nil
	},
}

// splitPlugins splits a comma-separated plugin list string into a []string slice.
func splitPlugins(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if entry := s[start:i]; entry != "" {
				result = append(result, entry)
			}
			start = i + 1
		}
	}
	return result
}

// storageForCheck resolves the storage and chart version `install` would use.
// Any failure degrades to zero values, so the media check reports "not determined".
func storageForCheck() (models.BlockNodeStorage, string) {
	if err := initializeDependencies(); err != nil {
		logx.As().Warn().Err(err).
			Msg("Storage media check skipped: failed to initialise block node dependencies")
		return models.BlockNodeStorage{}, ""
	}
	return resolveStorageForCheck(blockNodeHandler.Runtime(), storageFromFlags())
}

// resolveStorageForCheck seeds rt the way `install` does and returns the
// effective storage and chart version. Failures degrade to zero values.
func resolveStorageForCheck(rt *rsl.BlockNodeRuntimeResolver, flags models.BlockNodeStorage) (models.BlockNodeStorage, string) {
	// WithUserInputs drops storage that fails validation silently; say so.
	if err := flags.Validate(); err != nil {
		logx.As().Warn().Err(err).
			Msg("Ignoring storage flags for the storage media check: they did not validate")
	}

	// The chart-version resolver needs an intent; install's makes the deployed
	// version win on a deployed node.
	rt.WithIntent(models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode}).
		WithUserInputs(models.BlockNodeInputs{Storage: flags})

	effStorage, err := rt.Storage()
	if err != nil {
		logx.As().Warn().Err(err).
			Msg("Storage media check skipped: failed to resolve block node storage")
		return models.BlockNodeStorage{}, ""
	}

	effChartVersion, err := rt.ChartVersion()
	if err != nil {
		logx.As().Warn().Err(err).
			Msg("Storage media check skipped: failed to resolve block node chart version")
		return models.BlockNodeStorage{}, ""
	}

	chartVersion := effChartVersion.Get().Val()
	if chartVersion == "" {
		// An empty version (StrategyZero) isn't caught by the error branch above,
		// but would make GetApplicableOptionalStorages silently drop every optional
		// volume; treat it as a failure to resolve instead.
		logx.As().Warn().
			Msg("Storage media check skipped: no block node chart version could be determined")
		return models.BlockNodeStorage{}, ""
	}

	return effStorage.Get().Val(), chartVersion
}
