// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"testing"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestEnvironmentFlagRegistered(t *testing.T) {
	env := GetCmd().PersistentFlags().Lookup(common.FlagAlloyEnvironment().Name)
	require.NotNil(t, env, "--environment must be registered on the alloy cluster command")
	require.False(t, env.Hidden, "--environment must be visible in --help")
	require.Equal(t, common.FlagAlloyEnvironment().Description, env.Usage)
}

// runInstallUntilExecMode runs install's RunE with conflicting execution-mode flags,
// so a passing validation stops at GetExecutionMode instead of starting the workflow.
func runInstallUntilExecMode(t *testing.T, fileEnv, flagEnv string) error {
	t.Helper()

	saved := config.Get()
	savedEnv, savedStop, savedCont := flagEnvironment, flagStopOnError, flagContinueOnError
	t.Cleanup(func() {
		_ = config.Set(&saved)
		flagEnvironment, flagStopOnError, flagContinueOnError = savedEnv, savedStop, savedCont
	})

	cfg := config.Get()
	cfg.Alloy = models.AlloyConfig{Environment: fileEnv}
	require.NoError(t, config.Set(&cfg))
	flagEnvironment = flagEnv
	flagStopOnError, flagContinueOnError = true, true

	require.NoError(t, installCmd.ParseFlags(nil))
	return installCmd.RunE(installCmd, nil)
}

func TestInstall_ValidatesConfigFileEnvironment(t *testing.T) {
	const execModeErr = "only one of execution mode can be set"

	tests := []struct {
		name    string
		fileEnv string
		flagEnv string
		wantErr string
	}{
		{name: "invalid config-file value is rejected", fileEnv: `staging"`, wantErr: "invalid environment"},
		{name: "valid config-file value passes", fileEnv: "staging", wantErr: execModeErr},
		{name: "flag replaces invalid config-file value", fileEnv: `staging"`, flagEnv: "qa", wantErr: execModeErr},
		{name: "invalid flag value is rejected", flagEnv: `qa"`, wantErr: "invalid environment"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runInstallUntilExecMode(t, tt.fileEnv, tt.flagEnv)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
