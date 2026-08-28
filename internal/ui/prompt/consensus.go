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
			Description:    "Path to an extracted HIP-1494 deployment package (leave blank to enter image/ledger details manually)",
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

// ConsensusNodeIdentityInputPrompts returns the prompts for the node identity
// that would otherwise be extracted from a deployment package: image repo/tag,
// ledger ID, and (optional) chain ID. The caller presents these only when no
// --deployment-package-dir was supplied, so an operator without a package can
// still install without hunting for the right flags.
//
//   - flagImageRepo/flagImageTag: required image coordinates
//   - flagLedgerId:               required hex ledger identity (e.g. 0x00)
//   - flagChainId:                optional decimal EVM chain ID
func ConsensusNodeIdentityInputPrompts(flagImageRepo, flagImageTag, flagLedgerId, flagChainId *string) []InputPrompt {
	return []InputPrompt{
		{
			FlagName:       "image-repo",
			Title:          "Consensus Node Image Repository",
			Description:    "Container image repository for the consensus node",
			Placeholder:    *flagImageRepo,
			EffectiveValue: *flagImageRepo,
			Target:         flagImageRepo,
			Validate: func(s string) error {
				if s == "" {
					return errorx.IllegalArgument.New("image repository cannot be empty (or pass --deployment-package-dir)")
				}
				return nil
			},
		},
		{
			FlagName:       "image-tag",
			Title:          "Consensus Node Image Tag",
			Description:    "Container image tag for the consensus node",
			Placeholder:    *flagImageTag,
			EffectiveValue: *flagImageTag,
			Target:         flagImageTag,
			Validate: func(s string) error {
				if s == "" {
					return errorx.IllegalArgument.New("image tag cannot be empty (or pass --deployment-package-dir)")
				}
				return nil
			},
		},
		{
			FlagName:       "ledger-id",
			Title:          "Ledger ID",
			Description:    "Hex ledger identity (e.g. 0x00 for mainnet, 0x01 for local/dev)",
			Placeholder:    *flagLedgerId,
			EffectiveValue: *flagLedgerId,
			Target:         flagLedgerId,
			Validate: func(s string) error {
				if s == "" {
					return errorx.IllegalArgument.New("ledger ID cannot be empty (or pass --deployment-package-dir)")
				}
				return nil
			},
		},
		{
			FlagName:       "chain-id",
			Title:          "Chain ID",
			Description:    "Decimal EVM chain ID (optional, e.g. 295 for mainnet, 298 for local/dev)",
			Placeholder:    *flagChainId,
			EffectiveValue: *flagChainId,
			Target:         flagChainId,
		},
	}
}
