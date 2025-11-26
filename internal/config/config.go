package config

import (
	"os"
	"strings"

	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	"github.com/spf13/viper"
)

// Config holds the global configuration for the application.
type Config struct {
	Log       logx.LoggingConfig `yaml:"log" json:"log"`
	BlockNode BlockNodeConfig    `yaml:"blockNode" json:"blockNode"`
}

// BlockNodeStorage represents the `storage` section under `blockNode`.
type BlockNodeStorage struct {
	BasePath string `yaml:"basePath" json:"basePath"`
}

// BlockNodeConfig represents the `blockNode` configuration block.
type BlockNodeConfig struct {
	Namespace string           `yaml:"namespace" json:"namespace"`
	Release   string           `yaml:"release" json:"release"`
	Chart     string           `yaml:"chart" json:"chart"`
	Version   string           `yaml:"version" json:"version"`
	Storage   BlockNodeStorage `yaml:"storage" json:"storage"`
}

var globalConfig = Config{
	Log: logx.LoggingConfig{
		Level:          "Debug",
		ConsoleLogging: true,
		FileLogging:    false,
	},
	BlockNode: BlockNodeConfig{
		Namespace: "block-node",
		Release:   "block-node",
		Chart:     "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server",
		Version:   "0.22.1",
		Storage: BlockNodeStorage{
			BasePath: "/mnt/fast-storage",
		},
	},
}

// Initialize loads the configuration from the specified file.
//
// Parameters:
//   - path: The path to the configuration file.
//
// Returns:
//   - An error if the configuration cannot be loaded.
func Initialize(path string) error {
	if path != "" {
		viper.Reset()
		viper.SetConfigFile(path)
		viper.SetEnvPrefix("weaver")
		viper.AutomaticEnv()
		viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

		err := viper.ReadInConfig()
		if err != nil {
			return NotFoundError.Wrap(err, "failed to read config file: %s", path).
				WithProperty(errorx.PropertyPayload(), path)
		}

		if err := viper.Unmarshal(&globalConfig); err != nil {
			return errorx.IllegalFormat.Wrap(err, "failed to parse configuration").
				WithProperty(errorx.PropertyPayload(), path)
		}
	}

	return nil
}

// overrideWithEnv overrides configuration values with environment variables.
func overrideWithEnv(value string) string {
	if envValue := os.Getenv(value); envValue != "" {
		return envValue
	}
	return value
}

// Get returns the loaded configuration.
//
// Returns:
//   - The global configuration.
func Get() Config {
	return globalConfig
}

func Set(c *Config) error {
	globalConfig = *c
	return nil
}
