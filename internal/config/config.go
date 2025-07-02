package config

import (
	"fmt"
	"github.com/spf13/viper"
	"golang.hedera.com/solo-provisioner/pkg/logx"
	"os"
	"strings"
)

// Config holds the global configuration for the application.
type Config struct {
	// Log contains logging-related configuration.
	Log *logx.LoggingConfig
}

var config = Config{
	Log: &logx.LoggingConfig{
		Level:          "Info",
		ConsoleLogging: true,
		FileLogging:    false,
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
	viper.Reset()
	viper.SetConfigFile(path)
	viper.SetEnvPrefix("solo_provisioner")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to read configuration file: %w", err)
	}

	if err := viper.Unmarshal(&config); err != nil {
		return fmt.Errorf("failed to unmarshal configuration: %w", err)
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
	return config
}

func Set(c *Config) error {
	config = *c
	return nil
}
