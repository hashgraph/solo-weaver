// SPDX-License-Identifier: Apache-2.0

package hardware

import "github.com/hashgraph/solo-weaver/pkg/models"

func init() {
	registerProvider(models.NodeTypeConsensus, &consensusNodeProvider{})
}

type consensusNodeProvider struct{}

func (p *consensusNodeProvider) Compute(spec DeploymentSpec) (BaselineRequirements, error) {
	req, _, err := Reduce(consensusNodeRules, spec)
	if err != nil {
		return BaselineRequirements{}, err
	}
	req.MinSupportedOS = consensusNodeSupportedOS
	return req, nil
}

func (p *consensusNodeProvider) ComputeWithWhy(spec DeploymentSpec) (BaselineRequirements, map[string]string, error) {
	req, why, err := Reduce(consensusNodeRules, spec)
	if err != nil {
		return BaselineRequirements{}, nil, err
	}
	req.MinSupportedOS = consensusNodeSupportedOS
	return req, why, nil
}

var consensusNodeSupportedOS = []string{OSUbuntu18, OSDebian10}

var consensusNodeRules = []Rule{
	// local profile — minimal dev setup
	{
		When: profilePredicate(models.ProfileLocal),
		Then: Contribution{CpuCores: 3, MemoryGB: 1, StorageGB: 1, Why: "consensus node local development minimum"},
	},
	// perfnet / testnet — standard network floor
	{
		When: anyProfile(models.ProfilePerfnet, models.ProfileTestnet),
		Then: Contribution{CpuCores: 16, MemoryGB: 32, StorageGB: 1000, Why: "consensus node test network baseline"},
	},
	// previewnet / mainnet — high-performance production floor
	{
		When: anyProfile(models.ProfilePreviewnet, models.ProfileMainnet),
		Then: Contribution{CpuCores: 48, MemoryGB: 256, StorageGB: 8000, Why: "consensus node production baseline"},
	},
}
