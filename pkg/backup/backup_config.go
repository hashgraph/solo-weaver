package backup

// Backup is the configuration which defines the behaviors of the backup.Manager implementation.
type Backup struct {
	Pruning   Pruning  `yaml:"pruning" mapstructure:"pruning" json:"pruning"`
	Snapshots Snapshot `yaml:"snapshots" mapstructure:"snapshots" json:"snapshots"`
}

// Pruning BackupPruning provides the configuration for the pruning logic.
type Pruning struct {
	MaxAge    int `yaml:"max_age" mapstructure:"max_age" json:"max_age"`
	MaxCopies int `yaml:"max_copies" mapstructure:"max_copies" json:"max_copies"`
}

// Snapshot BackupSnapshot provides the configuration for the snapshot logic.
type Snapshot struct {
	Rules SnapshotRules `yaml:"rules" mapstructure:"rules" json:"rules"`
}

// SnapshotRules BackupSnapshotRules provides the BackupSnapshotRuleSet for the first time creation of a snapshot (eg: when the target is a directory
// and is not a symlink) and for the subsequent snapshots (eg: when the target is a symlink).
type SnapshotRules struct {
	// Creation is the BackupSnapshotRuleSet for the first time creation of a snapshot (eg: when the target is a
	// directory).
	//
	// This configuration maps to the bkp_folder_enable_snapshot_support method in the BASH code.
	Creation SnapshotRuleSet `yaml:"creation" mapstructure:"creation" json:"creation"`
	// Subsequent is the BackupSnapshotRuleSet for the subsequent snapshots after the first (eg: when the target is a
	// symlink).
	//
	// This configuration maps to the bkp_folder_create_snapshot method in the BASH code.
	Subsequent SnapshotRuleSet `yaml:"subsequent" mapstructure:"subsequent" json:"subsequent"`
}

// SnapshotRuleSet BackupSnapshotRuleSet provides the include and exclude patterns for a given rule.
type SnapshotRuleSet struct {
	Include []string `yaml:"include" mapstructure:"include" json:"include"`
	Exclude []string `yaml:"exclude" mapstructure:"exclude" json:"exclude"`
}
