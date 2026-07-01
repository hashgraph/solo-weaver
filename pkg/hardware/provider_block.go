// SPDX-License-Identifier: Apache-2.0

package hardware

import "github.com/hashgraph/solo-weaver/pkg/models"

func init() {
	registerProvider(models.NodeTypeBlock, &blockNodeProvider{})
}

type blockNodeProvider struct{}

func (p *blockNodeProvider) Compute(spec DeploymentSpec) (BaselineRequirements, error) {
	req, _, err := Reduce(blockNodeRules, spec)
	if err != nil {
		return BaselineRequirements{}, err
	}
	req.MinSupportedOS = blockNodeSupportedOS
	return req, nil
}

func (p *blockNodeProvider) ComputeWithWhy(spec DeploymentSpec) (BaselineRequirements, map[string]string, error) {
	req, why, err := Reduce(blockNodeRules, spec)
	if err != nil {
		return BaselineRequirements{}, nil, err
	}
	req.MinSupportedOS = blockNodeSupportedOS
	return req, why, nil
}

var blockNodeSupportedOS = []string{OSUbuntu18, OSDebian10}

// blockNodeRules encode hardware floors per (profile, preset) pair.
//
// LFH and RFH rules are mutually exclusive via not(presetPredicate("tier1-rfh")):
// RFH has lower CPU/memory/disk than LFH, so a shared baseline would prevent the
// Max reducer from ever landing on the smaller RFH numbers.
var blockNodeRules = []Rule{
	// local profile — minimal dev setup
	{
		When: profilePredicate(models.ProfileLocal),
		Then: Contribution{CpuCores: 3, MemoryGB: 1, StorageGB: 1, Why: "block node local development minimum"},
	},

	// testnet / perfnet — LFH (n2d-standard-16): default when no preset or tier1-lfh
	{
		When: and(anyProfile(models.ProfileTestnet, models.ProfilePerfnet), not(presetPredicate("tier1-rfh"))),
		Then: Contribution{CpuCores: 16, MemoryGB: 64, StorageGB: 5000, Why: "block node testnet LFH: n2d-standard-16, 5 TB local disk"},
	},
	// testnet / perfnet — RFH (c3d-standard-8)
	{
		When: and(anyProfile(models.ProfileTestnet, models.ProfilePerfnet), presetPredicate("tier1-rfh")),
		Then: Contribution{CpuCores: 8, MemoryGB: 32, StorageGB: 150, Why: "block node testnet RFH: c3d-standard-8, 150 GB local disk"},
	},

	// previewnet — LFH (n2d-standard-16): default when no preset or tier1-lfh
	{
		When: and(profilePredicate(models.ProfilePreviewnet), not(presetPredicate("tier1-rfh"))),
		Then: Contribution{CpuCores: 16, MemoryGB: 64, StorageGB: 3000, Why: "block node previewnet LFH: n2d-standard-16, 3 TB local disk"},
	},
	// previewnet — RFH (c3d-standard-8)
	{
		When: and(profilePredicate(models.ProfilePreviewnet), presetPredicate("tier1-rfh")),
		Then: Contribution{CpuCores: 8, MemoryGB: 32, StorageGB: 150, Why: "block node previewnet RFH: c3d-standard-8, 150 GB local disk"},
	},

	// mainnet — cloud RFH (n2d-highmem-32).
	// LFH runs on bare metal (lfh_count=0) and is outside the scope of provisioner hardware checks;
	// when no preset or tier1-lfh is set, no rule fires → all checks trivially pass (no-op).
	{
		When: and(profilePredicate(models.ProfileMainnet), presetPredicate("tier1-rfh")),
		Then: Contribution{CpuCores: 32, MemoryGB: 256, StorageGB: 150, Why: "block node mainnet cloud minimum: n2d-highmem-32, 150 GB local disk"},
	},
}
