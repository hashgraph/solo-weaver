// SPDX-License-Identifier: Apache-2.0

// Package config migration.go contains backward compatibility logic for deprecated config fields.
// This file can be safely deleted once users have migrated to the new config format.
//
// Deprecated fields (remove support after v1.0.0 or similar milestone):
//   - blockNode.release  -> blockNode.releaseName
//   - blockNode.chart    -> blockNode.chartRepo

package config

import (
	"github.com/automa-saga/logx"
	"github.com/spf13/viper"
)

// deprecatedKeyMappings defines the mapping from old (deprecated) keys to new keys.
// Add new migrations here as needed.
var deprecatedKeyMappings = []struct {
	oldKey string
	newKey string
}{
	{"blockNode.release", "blockNode.releaseName"},
	{"blockNode.chart", "blockNode.chartRepo"},
}

// migrateOldConfigKeys migrates deprecated config field names to their new names.
// This provides backward compatibility for users with old config files.
// It also logs deprecation warnings to encourage users to update their config files.
func migrateOldConfigKeys() {
	for _, mapping := range deprecatedKeyMappings {
		migrateKey(mapping.oldKey, mapping.newKey)
	}
}

// migrateKey migrates a single deprecated key to its new name.
// It only migrates if the new key is not already set and the old key exists.
// Logs a deprecation warning when migration occurs.
func migrateKey(oldKey, newKey string) {
	if !viper.IsSet(newKey) && viper.IsSet(oldKey) {
		value := viper.Get(oldKey)
		viper.Set(newKey, value)

		logx.As().Warn().
			Str("oldKey", oldKey).
			Str("newKey", newKey).
			Msg("DEPRECATION WARNING: Config field is deprecated and will be removed in a future release. Please update your config file.")
	}
}
