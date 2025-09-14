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
	Log      logx.LoggingConfig `yaml:"log" json:"log"`
	Versions VersionConfig      `yaml:"versions" json:"versions"`
}

var config = Config{
	Log: logx.LoggingConfig{
		Level:          "Info",
		ConsoleLogging: true,
		FileLogging:    false,
	},
	Versions: VersionConfig{
		Crio:       "1.33.4",
		Kubernetes: "1.33.4",
		Krel:       "v0.18.0",
		K9s:        "0.50.9",
		Helm:       "3.18.6",
		CiliumCli:  "0.18.7",
		Cilium:     "1.18.1",
		Metallb:    "2.8.1",
	},
}

type VersionConfig struct {
	Crio       string `yaml:"crio" json:"crio"`
	Kubernetes string `yaml:"kubernetes" json:"kubernetes"`
	Krel       string `yaml:"krel" json:"krel"`
	K9s        string `yaml:"k9s" json:"k9s"`
	Helm       string `yaml:"helm" json:"helm"`
	CiliumCli  string `yaml:"ciliumCli" json:"ciliumCli"`
	Cilium     string `yaml:"cilium" json:"cilium"`
	Metallb    string `yaml:"metallb" json:"metallb"`
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
