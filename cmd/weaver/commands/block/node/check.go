// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/hardware"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Runs safety checks to validate system readiness for Hedera Block node",
	Long:  "Runs safety checks to validate system readiness for deploying Hedera Block node",
	RunE: func(cmd *cobra.Command, args []string) error {
		flagProfile, err := common.FlagProfile.Value(cmd, args)
		if err != nil {
			return errorx.IllegalArgument.Wrap(err, "failed to get profile flag")
		}

		if flagProfile == "" {
			return errorx.IllegalArgument.New("profile flag is required")
		}

		// Validate profile early for better error messages
		if !hardware.IsValidProfile(flagProfile) {
			return errorx.IllegalArgument.New("unsupported profile: %q. Supported profiles: %v",
				flagProfile, hardware.SupportedProfiles())
		}

		// Set the profile in the global config so other components can access it
		config.SetProfile(flagProfile)

		logx.As().Debug().
			Strs("args", args).
			Str("nodeType", nodeType).
			Str("profile", flagProfile).
			Msg("Running preflight checks for Hedera Block Node")

		common.RunWorkflow(cmd.Context(), workflows.NewBlockNodePreflightCheckWorkflow(flagProfile))

		logx.As().Info().Msg("Node preflight checks completed successfully for block node")
		return nil
	},
}
