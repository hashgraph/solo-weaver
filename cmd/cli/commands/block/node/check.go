// SPDX-License-Identifier: Apache-2.0

package node

import (
	"strings"

	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	blocknode "github.com/hashgraph/solo-weaver/internal/blocknode"
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

		if err := common.RunWorkflowBuilder(cmd.Context(), workflows.NewBlockNodePreflightCheckWorkflow(deploySpec)); err != nil {
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
