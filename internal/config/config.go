package config

import (
	"github.com/joomcode/errorx"
	"github.com/spf13/viper"
	"golang.hedera.com/solo-provisioner/pkg/logx"
	"os"
	"strings"
)

// Config holds the global configuration for the application.
type Config struct {
	Log logx.LoggingConfig `yaml:"log" json:"log"`
}

var config = Config{
	Log: logx.LoggingConfig{
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
	if path == "" {
		return errorx.IllegalArgument.New("config file path cannot be empty").
			WithProperty(errorx.PropertyPayload(), "--config")
	}

	viper.Reset()
	viper.SetConfigFile(path)
	viper.SetEnvPrefix("solo_provisioner")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if err := viper.ReadInConfig(); err != nil {
		return NotFoundError.Wrap(err, "failed to read config file: %s", path).
			WithProperty(errorx.PropertyPayload(), path)
	}

	if err := viper.Unmarshal(&config); err != nil {
		return errorx.IllegalFormat.Wrap(err, "failed to parse configuration").
			WithProperty(errorx.PropertyPayload(), path)
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
