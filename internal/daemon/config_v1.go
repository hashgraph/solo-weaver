// SPDX-License-Identifier: Apache-2.0

package daemon

// daemonConfigV1 is the on-disk representation for schemaVersion: 1.
//
// Backward compatibility: the daemon loader is non-strict (plain yaml.Unmarshal,
// not KnownFields), and the schema version is resolved by the probe in
// LoadDaemonConfig, not by this struct's tag. A pre-rename file carrying the
// legacy `schema_version` key therefore still loads correctly: the unknown key
// is ignored, the probe sees no `schemaVersion` and normalises the absent value
// to 1. No migration step is required for the key rename.
// This struct is sealed — never modify it after it ships. When a breaking
// structural change is needed, add daemonConfigV2 and write
// daemonConfigV1.migrate() → daemonConfigV2, then update migrateToLatest()
// to delegate down the chain.
//
// Field layout must match the YAML written by WriteDaemonConfig at the time
// schemaVersion 1 was current.
type daemonConfigV1 struct {
	SchemaVersion int                `yaml:"schemaVersion"`
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
	Statusz    *statuszConfigV1    `yaml:"statusz,omitempty"`
}

type blockNodeMonitorsV1 struct {
	TrafficShaper bool `yaml:"traffic_shaper"`
}

type statuszConfigV1 struct {
	BaseURL      string `yaml:"base_url,omitempty"`
	PollInterval string `yaml:"poll_interval,omitempty"`
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
		blockNode := &BlockNodeComponentConfig{
			Enabled:    bn.Enabled,
			Kubeconfig: bn.Kubeconfig,
			Orbit:      bn.Orbit,
			Monitors: BlockNodeMonitors{
				TrafficShaper: bn.Monitors.TrafficShaper,
			},
		}
		if s := bn.Statusz; s != nil {
			blockNode.Statusz = &StatuszConfig{
				BaseURL:      s.BaseURL,
				PollInterval: s.PollInterval,
			}
		}
		cfg.Components.BlockNode = blockNode
	}
	return cfg
}
