// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"testing"

	"github.com/automa-saga/errx"
	"github.com/joomcode/errorx"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
)

func TestEnvironmentFlagRegistered(t *testing.T) {
	env := GetCmd().PersistentFlags().Lookup(common.FlagAlloyEnvironment().Name)
	require.NotNil(t, env, "--environment must be registered on the alloy cluster command")
	require.False(t, env.Hidden, "--environment must be visible in --help")
	require.Equal(t, common.FlagAlloyEnvironment().Description, env.Usage)
}

// runInstallUntilExecMode runs install's RunE with file as the --config alloy
// block and args as the command line. Conflicting execution-mode flags stop a
// passing run at GetExecutionMode, before the workflow starts.
func runInstallUntilExecMode(t *testing.T, file models.AlloyConfig, args ...string) error {
	t.Helper()

	saved := config.Get()
	savedStop, savedCont := flagStopOnError, flagContinueOnError
	t.Cleanup(func() {
		_ = config.Set(&saved)
		flagStopOnError, flagContinueOnError = savedStop, savedCont
		installCmd.Flags().Visit(resetFlag)
	})

	cfg := config.Get()
	cfg.Alloy = file
	require.NoError(t, config.Set(&cfg))
	flagStopOnError, flagContinueOnError = true, true

	require.NoError(t, installCmd.ParseFlags(args))
	return installCmd.RunE(installCmd, nil)
}

// resetFlag undoes a parse: pflag has no reset, and Changed would leak into later tests.
func resetFlag(f *pflag.Flag) {
	if sv, ok := f.Value.(pflag.SliceValue); ok {
		_ = sv.Replace(nil)
	} else {
		_ = f.Value.Set(f.DefValue)
	}
	f.Changed = false
}

const execModeErr = "only one of execution mode can be set"

func TestInstall_ValidatesConfigFileEnvironment(t *testing.T) {
	tests := []struct {
		name    string
		fileEnv string
		args    []string
		wantErr string
	}{
		{name: "invalid config-file value is rejected", fileEnv: `staging"`, wantErr: "invalid environment"},
		{name: "valid config-file value passes", fileEnv: "staging", wantErr: execModeErr},
		{name: "flag replaces invalid config-file value", fileEnv: `staging"`, args: []string{"--environment=qa"}, wantErr: execModeErr},
		{name: "invalid flag value is rejected", args: []string{`--environment=qa"`}, wantErr: "invalid environment"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runInstallUntilExecMode(t, models.AlloyConfig{Environment: tt.fileEnv}, tt.args...)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestInstall_MonitorBlockNodeFlagOverridesFile(t *testing.T) {
	file := models.AlloyConfig{
		MonitorBlockNode:  true,
		PrometheusRemotes: []models.AlloyRemoteConfig{{Name: "primary", URL: "http://prom:9090/api/v1/write"}},
		LokiRemotes:       []models.AlloyRemoteConfig{{Name: "primary", URL: "http://loki:3100/loki/api/v1/push"}},
	}

	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "no flag keeps the file's true", want: true},
		{name: "--monitor-block-node=false turns it off", args: []string{"--monitor-block-node=false"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runInstallUntilExecMode(t, file, tt.args...)
			require.ErrorContains(t, err, execModeErr)
			got := config.Get().Alloy
			assert.Equal(t, tt.want, got.MonitorBlockNode)
			assert.Equal(t, file.PrometheusRemotes, got.PrometheusRemotes)
			assert.Equal(t, file.LokiRemotes, got.LokiRemotes)
		})
	}
}

func TestInstall_RejectsInvalidConfigFileRemote(t *testing.T) {
	file := models.AlloyConfig{
		PrometheusRemotes: []models.AlloyRemoteConfig{{Name: "bad name", URL: "http://prom:9090/api/v1/write"}},
	}

	err := runInstallUntilExecMode(t, file)
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.IllegalArgument), "got %v", err)
	assert.Equal(t, "invalid alloy settings in --config", errorx.Cast(err).Message())
	assert.Contains(t, err.Error(), "prometheus remote[0]: invalid name: bad name")

	reason, ok := errx.ReasonOf(err)
	require.True(t, ok)
	assert.Equal(t, reasons.InvalidArgument, reason)
}
