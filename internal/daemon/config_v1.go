// SPDX-License-Identifier: Apache-2.0

package daemon

// daemonConfigV1 is the on-disk representation for schema_version: 1.
// This struct is sealed — never modify it after it ships. When a breaking
// structural change is needed, add daemonConfigV2 and write
// daemonConfigV1.migrate() → daemonConfigV2, then update migrateToLatest()
// to delegate down the chain.
//
// Field layout must match the YAML written by WriteDaemonConfig at the time
// schema_version 1 was current.
type daemonConfigV1 struct {
	SchemaVersion int                `yaml:"schema_version"`
	Components    daemonComponentsV1 `yaml:"components"`
}

type daemonComponentsV1 struct {
	ConsensusNode *consensusNodeConfigV1 `yaml:"consensus_node,omitempty"`
	BlockNode     *blockNodeConfigV1     `yaml:"block_node,omitempty"`
}

type consensusNodeConfigV1 struct {
	Enabled    bool                    `yaml:"enabled"`
	Kubeconfig string                  `yaml:"kubeconfig"`
	NodeID     string                  `yaml:"node_id"`
	Orbit      string                  `yaml:"orbit"`
	UpgradeDir string                  `yaml:"upgrade_dir,omitempty"`
	Monitors   consensusNodeMonitorsV1 `yaml:"monitors"`
}

type consensusNodeMonitorsV1 struct {
	Upgrade   bool `yaml:"upgrade"`
	Migration bool `yaml:"migration"`
}

type blockNodeConfigV1 struct {
	Enabled    bool                `yaml:"enabled"`
	Kubeconfig string              `yaml:"kubeconfig"`
	Orbit      string              `yaml:"orbit"`
	Monitors   blockNodeMonitorsV1 `yaml:"monitors"`
}

type blockNodeMonitorsV1 struct {
	Upgrade bool `yaml:"upgrade"`
}

// migrateToLatest is the terminal step of the migration chain at v1.
// When v2 is introduced:
//  1. Add migrate() daemonConfigV2 to this type (one-step transform only).
//  2. Change this body to: return v.migrate().migrateToLatest()
//  3. Add daemonConfigV2.migrateToLatest() as the new terminal.
func (v daemonConfigV1) migrateToLatest() DaemonConfig {
	cfg := DaemonConfig{
		SchemaVersion: CurrentSchemaVersion,
	}
	if cn := v.Components.ConsensusNode; cn != nil {
		cfg.Components.ConsensusNode = &ConsensusNodeComponentConfig{
			Enabled:    cn.Enabled,
			Kubeconfig: cn.Kubeconfig,
			NodeID:     cn.NodeID,
			Orbit:      cn.Orbit,
			UpgradeDir: cn.UpgradeDir,
			Monitors: ConsensusNodeMonitors{
				Upgrade:   cn.Monitors.Upgrade,
				Migration: cn.Monitors.Migration,
			},
		}
	}
	if bn := v.Components.BlockNode; bn != nil {
		cfg.Components.BlockNode = &BlockNodeComponentConfig{
			Enabled:    bn.Enabled,
			Kubeconfig: bn.Kubeconfig,
			Orbit:      bn.Orbit,
			Monitors: BlockNodeMonitors{
				Upgrade: bn.Monitors.Upgrade,
			},
		}
	}
	return cfg
}
