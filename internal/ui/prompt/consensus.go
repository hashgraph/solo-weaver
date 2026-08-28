// SPDX-License-Identifier: Apache-2.0

package prompt

import (
	"strconv"

	"github.com/joomcode/errorx"

	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
)

// ConsensusNodeSelectPrompts returns the select-type prompts for the consensus
// node install command. Currently only the deployment profile, which sizes the
// host hardware floor when the install has to bootstrap the cluster.
//
//   - flagProfile: pointer to the flag variable for --profile
func ConsensusNodeSelectPrompts(flagProfile *string) []SelectPrompt {
	effectiveProfile := resolveEffective("", config.Get().Profile, "")

	return []SelectPrompt{
		{
			FlagName:       "profile",
			Title:          "Deployment Profile",
			Description:    "Select the target deployment profile — sizes the host hardware floor when this install bootstraps the cluster",
			Options:        models.SupportedProfiles(),
			EffectiveValue: effectiveProfile,
			Target:         flagProfile,
		},
	}
}

// ConsensusNodeInputPrompts returns the text-input prompts for the consensus
// node install command: the core identity fields an operator typically sets.
// The remaining values (ledger-id, chain-id, image repo/tag, secret names) are
// auto-resolved from the deployment package and are not prompted.
//
// node-id is an int64 flag, so its prompt writes into a string target which the
// caller parses back after the wizard completes.
//
//   - flagNamespace:  pointer to the --namespace flag variable
//   - nodeIdTarget:   pointer to a string holding the --node-id value (parsed by the caller)
//   - flagAccountId:  pointer to the --account-id flag variable
//   - flagPkgDir:     pointer to the --deployment-package-dir flag variable
func ConsensusNodeInputPrompts(flagNamespace, nodeIdTarget, flagAccountId, flagPkgDir *string) []InputPrompt {
	return []InputPrompt{
		{
			FlagName:       "namespace",
			Title:          "Kubernetes Namespace",
			Description:    "Namespace for the consensus node (also used as the Orbit CR name)",
			Placeholder:    *flagNamespace,
			EffectiveValue: *flagNamespace,
			Target:         flagNamespace,
			Validate: func(s string) error {
				if s == "" {
					return errorx.IllegalArgument.New("namespace cannot be empty")
				}
				return sanity.ValidateIdentifier(s)
			},
		},
		{
			FlagName:       "node-id",
			Title:          "Consensus Node ID",
			Description:    "0-based consensus node ID",
			Placeholder:    *nodeIdTarget,
			EffectiveValue: *nodeIdTarget,
			Target:         nodeIdTarget,
			Validate: func(s string) error {
				n, err := strconv.ParseInt(s, 10, 64)
				if err != nil || n < 0 {
					return errorx.IllegalArgument.New("node ID must be a non-negative integer")
				}
				return nil
			},
		},
		{
			FlagName:       "account-id",
			Title:          "Node Account ID",
			Description:    "Hedera account ID for this node, e.g. 0.0.3",
			Placeholder:    *flagAccountId,
			EffectiveValue: *flagAccountId,
			Target:         flagAccountId,
			Validate: func(s string) error {
				if s == "" {
					return errorx.IllegalArgument.New("account ID cannot be empty")
				}
				return nil
			},
		},
		{
			FlagName:       "deployment-package-dir",
			Title:          "Deployment Package Directory",
			Description:    "Path to an extracted HIP-1494 deployment package (leave blank to use flags/embedded defaults)",
			Placeholder:    *flagPkgDir,
			EffectiveValue: *flagPkgDir,
			Target:         flagPkgDir,
			Validate: func(s string) error {
				if s == "" {
					return nil
				}
				_, err := sanity.SanitizePath(s)
				return err
			},
		},
	}
}
