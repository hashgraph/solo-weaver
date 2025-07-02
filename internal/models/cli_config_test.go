/*
 * Copyright 2016-2022 Hedera Hashgraph, LLC
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
 */

package models

import (
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/internal/otl"
	"os"
	"testing"
)

func TestConfig_WithEmbeddedConfigStr(t *testing.T) {
	var (
		config CLIConfig
		err    error
	)

	req := require.New(t)

	defer viper.Reset()

	embeddedConfig := `
log:
  max_age: 90 # Days
  max_size: 100 # MB
`

	viper.Reset()
	err = reloadConfig(newViper(ViperSettings{
		ConfigNames:     []string{},
		ConfigFilePaths: []string{},
	}), "", &config)
	req.NoError(err)
	req.Empty(config.Log)

	viper.Reset()
	err = reloadConfig(newViper(ViperSettings{
		ConfigNames:     []string{},
		ConfigFilePaths: []string{},
	}), embeddedConfig, &config)
	req.NoError(err)
	req.NotEmpty(config.Log)
}

func TestConfig_LoadCLIConfigFailures(t *testing.T) {
	var (
		config CLIConfig
		err    error
	)

	req := require.New(t)

	testDataDir := "../../../../test/data"
	defer viper.Reset()

	// invalid file should return empty
	viper.Reset()
	err = reloadConfig(newViper(ViperSettings{
		ConfigNames:     []string{"INVALID"},
		ConfigFilePaths: []string{testDataDir},
	}), "", &config)
	req.NoError(err)
	req.Empty(config.Log)

	// invalid path should return empty
	viper.Reset()
	err = reloadConfig(newViper(ViperSettings{
		ConfigNames:     []string{"nmt-cli"},
		ConfigFilePaths: []string{"/INVALID"},
	}), "", &config)
	req.NoError(err)
	req.Empty(config.Log)

	// invalid format should error
	viper.Reset()
	err = reloadConfig(newViper(ViperSettings{
		ConfigNames:     []string{"nmt-config-err"},
		ConfigFilePaths: []string{"/INVALID"},
	}), "", &config)
	req.NoError(err)
	req.Empty(config.Log)
}

func TestConfig_OverrideCLIConfig_WithConfigFile(t *testing.T) {
	testDataDir := "../../../../test/data"
	defer viper.Reset()

	var config CLIConfig
	req := require.New(t)

	// override existing path with new set of paths
	viper.Reset()
	expected := testCLIConfig(t)
	err := reloadConfig(newViper(ViperSettings{
		ConfigNames:     []string{"nmt-cli", "nmt-override2"},
		ConfigFilePaths: []string{testDataDir},
	}), "", &config)
	req.NoError(err)
	expected.Log.MaxAge = 13
	req.Equal(expected.Log, config.Log)
}

func TestConfig_OverrideCLIConfig_WithPropertiesFile(t *testing.T) {
	testDataDir := "../../../../test/data"
	defer viper.Reset()

	var config CLIConfig
	var err error
	req := require.New(t)

	// override with .properties file
	viper.Reset()
	expected := testCLIConfig(t)
	expected.Log.MaxAge = 7

	err = reloadConfig(newViper(ViperSettings{
		ConfigNames:     []string{"nmt-cli", "nmt_ics_prop"},
		ConfigFilePaths: []string{testDataDir},
		EnvPrefix:       "nmt",
	}), "", &config)
	req.NoError(err)
	require.Equal(t, expected.Log, config.Log)
}

func TestConfig_OverrideCLIConfig_WithEnvVar(t *testing.T) {
	testDataDir := "../../../../test/data"
	defer viper.Reset()

	var config CLIConfig
	var err error
	req := require.New(t)

	// override with env var
	viper.Reset()
	testOTelCollector := otl.OTelCollectorConfig{
		Endpoint:      "0.0.0.0.1234",
		RetryInterval: "15s",
	}

	os.Setenv("NMT_OTEL_COLLECTOR_ENDPOINT", testOTelCollector.Endpoint)
	defer os.Setenv("NMT_OTEL_COLLECTOR_ENDPOINT", "")

	os.Setenv("NMT_OTEL_COLLECTOR_RETRY_INTERVAL", testOTelCollector.RetryInterval)
	defer os.Setenv("NMT_OTEL_COLLECTOR_RETRY_INTERVAL", "")

	os.Setenv("NMT_OTEL_COLLECTOR_TLS_CA_FILE", "ca.crt")
	defer os.Setenv("NMT_OTEL_COLLECTOR_TLS_CAFILE", "")

	os.Setenv("NMT_LOG_MAX_AGE", "10")
	defer os.Setenv("NMT_LOG_MAX_AGE", "")

	expected := testCLIConfig(t)
	expected.Otel.Collector.Endpoint = testOTelCollector.Endpoint
	expected.Otel.Collector.RetryInterval = testOTelCollector.RetryInterval
	expected.Otel.Collector.TLS.CaFile = "ca.crt"
	expected.Log.MaxAge = 10

	err = reloadConfig(newViper(ViperSettings{
		ConfigNames:     []string{"nmt-cli"},
		ConfigFilePaths: []string{testDataDir},
		EnvPrefix:       "nmt",
	}), "", &config)
	req.NoError(err)
	require.Equal(t, expected.Otel, config.Otel)
	require.Equal(t, expected.Log, config.Log)

	// try a different prefix
	os.Setenv("NMT_CLI_LOG_MAX_AGE", "10")
	defer os.Setenv("NMT_CLI_LOG_MAX_AGE", "")
	err = reloadConfig(newViper(ViperSettings{
		ConfigNames:     []string{"nmt-cli"},
		ConfigFilePaths: []string{testDataDir},
		EnvPrefix:       "nmt_cli",
	}), "", &config)
	req.NoError(err)
	require.NotEqual(t, expected.Otel, config.Otel)
	require.Equal(t, expected.Log, config.Log)
}

func testCLIConfig(t *testing.T) *CLIConfig {
	return &CLIConfig{
		Otel: otl.OTel{
			Enable: true,
			Trace: otl.OTelTraceConfig{
				LogLevel: "DEBUG",
			},
			Collector: otl.OTelCollectorConfig{
				Endpoint:      "0.0.0.0:4317",
				RetryInterval: "15s",
				TLS: otl.OTelTLSConfig{
					Insecure:   true,
					CaFile:     "server.crt",
					CertFile:   "client.crt",
					KeyFile:    "client.key",
					MinVersion: "1.1",
					MaxVersion: "1.2",
				},
			},
		},
		Log: Log{
			MaxAge:  90,
			MaxSize: 100,
		},
	}
}
