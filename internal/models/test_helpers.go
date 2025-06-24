/*
 * Copyright 2016-2023 Hedera Hashgraph, LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 *
 *
 */

package models

import (
	"bytes"
	"github.com/spf13/viper"
	"strings"
)

// ViperSettings defines a structure for a config name and corresponding config file names.
type ViperSettings struct {
	ConfigNames     []string
	ConfigFilePaths []string
	EnvPrefix       string // e.g. lowercase string "nmt" would allow env prefix to be NMT_
}

// newViper sets up viper.Viper with necessary configuration to be able to load and merge the configs.
//
// Base config can be overridden with another config file (yaml, .properties etc.) or env vars (prefix = NMT)
// For example, NMT_NODE.ID env variable would override the Node.ID field. Similarly, node.id in properties file or .env
// file would override Node.ID Field.
func newViper(settings ViperSettings) *viper.Viper {
	v := viper.New()

	// set config file paths
	for _, path := range settings.ConfigFilePaths {
		v.AddConfigPath(strings.TrimSpace(path))
	}

	// load config files
	for _, name := range settings.ConfigNames {
		v.SetConfigName(name) // name of config file (without extension)

		// Note: we are skipping any error handling here to let it do best effort loading
		// This is because the configuration validation needs to be done by respective nmt commands (config users)
		_ = v.MergeInConfig()
	}

	// override config using the env variables
	if settings.EnvPrefix != "" {
		v.SetEnvPrefix(settings.EnvPrefix)
	}
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	_ = v.BindEnv()

	return v
}

// reloadConfig loads and merge the embedded config with config from the paths and env.
// It expects the embedded config string to be in YAML format.
func reloadConfig(v *viper.Viper, embeddedYAMLConfigStr string, config interface{}) error {
	if embeddedYAMLConfigStr != "" {
		v.SetConfigType("yaml")
		v.MergeConfig(bytes.NewReader([]byte(embeddedYAMLConfigStr)))
	}

	// prep config to be returned
	if err := v.Unmarshal(config); err != nil {
		return err
	}

	return nil
}
