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
	"context"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/internal/otl"
	"golang.hedera.com/solo-provisioner/pkg/events"
	"os"
	"testing"
)

func TestConfig_ICSConfig_WithEmbeddedConfigStr(t *testing.T) {
	var (
		config ICSConfig
		err    error
	)

	req := require.New(t)

	defer viper.Reset()

	embeddedConfig := `
daemon:
 watch:
  paths:
    - path: "{{.HederaAppDir}}/services-hedera/HapiApp2.0/data/upgrade/current"
      events:
        - CREATE
      filter: "*.mf"
      execute:
        command: "{{.NodeMgmtToolsDir}}/bin/nmt-incron-dispatch.sh"
        arguments:
          - "{{.EventName}}"
          - "{{.EventFile}}"
`

	viper.Reset()
	err = reloadConfig(newViper(ViperSettings{
		ConfigNames:     []string{},
		ConfigFilePaths: []string{},
	}), "", &config)
	req.NoError(err)
	req.Empty(config.Daemon.Watch.Paths)
	req.Empty(config.Log)

	viper.Reset()
	err = reloadConfig(newViper(ViperSettings{
		ConfigNames:     []string{},
		ConfigFilePaths: []string{},
	}), embeddedConfig, &config)
	req.NoError(err)
	req.NotEmpty(config.Daemon.Watch.Paths)
	req.Empty(config.Log)
}
func TestConfig_LoadICSConfigFailures(t *testing.T) {
	var (
		config ICSConfig
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
	req.Empty(config.Daemon.Watch.Paths)

	// invalid path should return empty
	viper.Reset()
	err = reloadConfig(newViper(ViperSettings{
		ConfigNames:     []string{"nmt-ics"},
		ConfigFilePaths: []string{"/INVALID"},
	}), "", &config)
	req.NoError(err)
	req.Empty(config.Daemon.Watch.Paths)

	// invalid format should error
	viper.Reset()
	err = reloadConfig(newViper(ViperSettings{
		ConfigNames:     []string{"nmt-config-err"},
		ConfigFilePaths: []string{"/INVALID"},
	}), "", &config)
	req.NoError(err)
	req.Empty(config.Daemon.Watch.Paths)
}

func TestConfig_OverrideICSConfig_WithConfigFile(t *testing.T) {
	testDataDir := "../../../../test/data"
	defer viper.Reset()

	var config ICSConfig
	var err error
	req := require.New(t)

	// override existing path with new set of paths
	viper.Reset()
	expected := testICSConfig(t)
	expected.Daemon.Watch.Paths = []WatchPath{
		{
			Path:   expected.Daemon.Watch.Paths[0].Path,
			Events: []events.WatchEvent{events.Create},
			Filter: "*.mf",
			Execute: Execute{
				Command: "ls",
				Arguments: []string{
					"-al",
				},
			},
		},
	}

	err = reloadConfig(newViper(ViperSettings{
		ConfigNames:     []string{"nmt-ics", "nmt-override"},
		ConfigFilePaths: []string{testDataDir},
	}), "", &config)
	req.NoError(err)
	req.Equal(expected.Daemon, config.Daemon)

	// override existing path with new node ID
	viper.Reset()
	err = reloadConfig(newViper(ViperSettings{
		ConfigNames:     []string{"nmt-ics", "nmt-override2"},
		ConfigFilePaths: []string{testDataDir},
	}), "", &config)
	expected.Log.MaxAge = 13
	req.Equal(expected.Log, config.Log)
}

func TestConfig_OverrideICSConfig_WithPropertiesFile(t *testing.T) {
	testDataDir := "../../../../test/data"
	defer viper.Reset()

	var config ICSConfig
	var err error
	req := require.New(t)

	// override with .properties file
	viper.Reset()
	expected := testICSConfig(t)
	expected.Log.MaxAge = 7

	err = reloadConfig(newViper(ViperSettings{
		ConfigNames:     []string{"nmt-ics", "nmt_ics_prop"},
		ConfigFilePaths: []string{testDataDir},
		EnvPrefix:       "nmt",
	}), "", &config)
	req.NoError(err)
	require.Equal(t, expected.Log, config.Log)
}

func TestConfig_OverrideICSConfig_WithEnvVar(t *testing.T) {
	testDataDir := "../../../../test/data"
	defer viper.Reset()

	var config ICSConfig
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

	expected := testICSConfig(t)
	expected.Otel.Collector.Endpoint = testOTelCollector.Endpoint
	expected.Otel.Collector.RetryInterval = testOTelCollector.RetryInterval
	expected.Otel.Collector.TLS.CaFile = "ca.crt"
	expected.Log.MaxAge = 10

	err = reloadConfig(newViper(ViperSettings{
		ConfigNames:     []string{"nmt-ics", "nmt_ics_prop"},
		ConfigFilePaths: []string{testDataDir},
		EnvPrefix:       "nmt",
	}), "", &config)
	req.NoError(err)
	require.Equal(t, expected.Otel, config.Otel)
	require.Equal(t, expected.Log, config.Log)
}

func TestConfig_ParseExecuteCommand(t *testing.T) {
	ctx := context.Background()
	req := require.New(t)

	wp := WatchPath{
		Path:   ".",
		Events: []events.WatchEvent{events.Create},
		Filter: ".tmp",
		Execute: Execute{
			Command:   "{{.replaceMe}}/test",
			Arguments: []string{"{{.arg1}}"},
		},
	}
	templateData := map[string]string{
		"replaceMe": "REPLACE_ME",
		"arg1":      "ARG1",
	}
	cmd, args, err := ParseExecuteCommand(ctx, wp, templateData)
	req.NoError(err)
	req.Equal("REPLACE_ME/test", cmd)
	req.Equal([]string{"ARG1"}, args)
}

func testICSConfig(t *testing.T) *ICSConfig {
	return &ICSConfig{
		Daemon: Daemon{
			Watch: Watch{
				Paths: []WatchPath{
					{
						Path:   "{{.HederaAppDir}}/services-hedera/HapiApp2.0/data/upgrade/current",
						Events: []events.WatchEvent{events.Create},
						Filter: "*.mf",
						Execute: Execute{
							Command: "{{.NodeMgmtToolsDir}}/bin/nmt-incron-dispatch.sh",
							Arguments: []string{
								"{{.EventName}}",
								"{{.EventFile}}",
							},
						},
					},
				},
			},
		},
		Otel: otl.OTel{
			Enable: false,
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
