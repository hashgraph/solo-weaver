// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/daemon"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func blockNodeConfigPaths(t *testing.T) models.WeaverPaths {
	t.Helper()
	dir := t.TempDir()
	return models.WeaverPaths{
		DaemonConfigPath:       filepath.Join(dir, "daemon.yaml"),
		DaemonBNKubeconfigPath: "/opt/solo/weaver/config/daemon-bn.kubeconfig",
	}
}

func TestWriteBlockNodeDaemonConfig_FreshFile(t *testing.T) {
	paths := blockNodeConfigPaths(t)

	step, err := WriteBlockNodeDaemonConfigStep(paths, "block-node").Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	cfg, err := daemon.LoadDaemonConfig(paths.DaemonConfigPath)
	require.NoError(t, err)
	require.NotNil(t, cfg.Components.BlockNode)
	assert.True(t, cfg.Components.BlockNode.Enabled)
	assert.True(t, cfg.Components.BlockNode.Monitors.TrafficShaper)
	assert.Equal(t, "block-node", cfg.Components.BlockNode.Orbit)
	assert.Equal(t, paths.DaemonBNKubeconfigPath, cfg.Components.BlockNode.Kubeconfig)

	// Rollback must remove the file it created.
	rb := step.Rollback(context.Background())
	require.NoError(t, rb.Error)
	_, statErr := os.Stat(paths.DaemonConfigPath)
	assert.True(t, os.IsNotExist(statErr), "rollback should remove the created daemon.yaml")
}

func TestWriteBlockNodeDaemonConfig_EmptyOrbitDefaults(t *testing.T) {
	paths := blockNodeConfigPaths(t)

	step, err := WriteBlockNodeDaemonConfigStep(paths, "").Build()
	require.NoError(t, err)
	require.NoError(t, step.Execute(context.Background()).Error)

	cfg, err := daemon.LoadDaemonConfig(paths.DaemonConfigPath)
	require.NoError(t, err)
	assert.Equal(t, defaultBlockNodeOrbit, cfg.Components.BlockNode.Orbit)
}

func TestWriteBlockNodeDaemonConfig_PreservesExistingBlocks(t *testing.T) {
	paths := blockNodeConfigPaths(t)

	// Seed an existing config: a consensus_node block plus a block_node with an
	// operator-set statusz local-fallback source.
	seed := daemon.DaemonConfig{
		Components: daemon.DaemonComponents{
			ConsensusNode: &daemon.ConsensusNodeComponentConfig{
				Enabled:    true,
				Kubeconfig: "/opt/solo/weaver/config/daemon-cn.kubeconfig",
				NodeID:     "3",
				Orbit:      "hedera-network",
				Monitors:   daemon.ConsensusNodeMonitors{Upgrade: true},
			},
			BlockNode: &daemon.BlockNodeComponentConfig{
				Statusz: &daemon.StatuszConfig{BaseURL: "http://127.0.0.1:8080", PollInterval: "3s"},
			},
		},
	}
	require.NoError(t, daemon.WriteDaemonConfig(paths.DaemonConfigPath, seed))
	priorBytes, err := os.ReadFile(paths.DaemonConfigPath)
	require.NoError(t, err)

	step, err := WriteBlockNodeDaemonConfigStep(paths, "block-node").Build()
	require.NoError(t, err)
	require.NoError(t, step.Execute(context.Background()).Error)

	cfg, err := daemon.LoadDaemonConfig(paths.DaemonConfigPath)
	require.NoError(t, err)
	// consensus_node preserved.
	require.NotNil(t, cfg.Components.ConsensusNode)
	assert.Equal(t, "3", cfg.Components.ConsensusNode.NodeID)
	// block_node enablement set, operator statusz preserved.
	require.NotNil(t, cfg.Components.BlockNode)
	assert.True(t, cfg.Components.BlockNode.Monitors.TrafficShaper)
	require.NotNil(t, cfg.Components.BlockNode.Statusz)
	assert.Equal(t, "http://127.0.0.1:8080", cfg.Components.BlockNode.Statusz.BaseURL)
	assert.Equal(t, "3s", cfg.Components.BlockNode.Statusz.PollInterval)

	// Rollback restores the exact prior file content.
	require.NoError(t, step.Rollback(context.Background()).Error)
	gotBytes, err := os.ReadFile(paths.DaemonConfigPath)
	require.NoError(t, err)
	assert.Equal(t, string(priorBytes), string(gotBytes))
}
