// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package daemon_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/daemon"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestLoadDaemonConfig_NewerSchemaVersion(t *testing.T) {
	// A daemon.yaml written by a future binary — schema_version 99 with unknown
	// top-level keys (components) that would cause a strict-decode error if the
	// version guard ran after the full unmarshal.
	yaml := `
schema_version: 99
components:
  consensus_node:
    enabled: true
    kubeconfig: /some/path
    node_id: "0"
    orbit: hedera-network
    monitors:
      upgrade: true
      migration: true
`
	path := writeTempConfig(t, yaml)

	_, err := daemon.LoadDaemonConfig(path)
	require.Error(t, err)

	// Must be the human-readable "newer binary" message, not a decode error.
	assert.True(t, errorx.IsOfType(err, daemon.ErrConfigMalformed),
		"expected ErrConfigMalformed, got %T: %v", err, err)
	assert.Contains(t, err.Error(), "newer binary",
		"error should mention 'newer binary', got: %s", err)
	assert.Contains(t, err.Error(), "99",
		"error should include the on-disk schema version")
	assert.NotContains(t, err.Error(), "invalid keys",
		"should not surface a raw decode error before the version guard")
}

func TestLoadDaemonConfig_ValidV1(t *testing.T) {
	yaml := `
schema_version: 1
components:
  consensus_node:
    enabled: true
    kubeconfig: /opt/solo/weaver/config/daemon.kubeconfig
    node_id: "3"
    orbit: hedera-network
    monitors:
      upgrade: true
      migration: false
`
	path := writeTempConfig(t, yaml)

	cfg, err := daemon.LoadDaemonConfig(path)
	require.NoError(t, err)
	require.NotNil(t, cfg.Components.ConsensusNode)
	assert.Equal(t, "3", cfg.Components.ConsensusNode.NodeID)
	assert.Equal(t, "hedera-network", cfg.Components.ConsensusNode.Orbit)
	assert.True(t, cfg.Components.ConsensusNode.Monitors.Upgrade)
	assert.False(t, cfg.Components.ConsensusNode.Monitors.Migration)
}

func TestLoadDaemonConfig_MissingFile(t *testing.T) {
	_, err := daemon.LoadDaemonConfig("/nonexistent/path/daemon.yaml")
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, daemon.ErrConfigNotFound),
		"expected ErrConfigNotFound, got %T: %v", err, err)
}

func TestLoadDaemonConfig_MalformedYAML(t *testing.T) {
	path := writeTempConfig(t, "schema_version: [not a number}")
	_, err := daemon.LoadDaemonConfig(path)
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, daemon.ErrConfigMalformed),
		"expected ErrConfigMalformed for invalid YAML, got %T: %v", err, err)
}

func TestLoadDaemonConfig_NoSchemaVersionTreatedAsV1(t *testing.T) {
	// Files predating schema versioning have no schema_version field.
	// They must be accepted as version 1.
	yaml := `
components:
  consensus_node:
    enabled: true
    kubeconfig: /opt/solo/weaver/config/daemon.kubeconfig
    node_id: "0"
    orbit: hedera-network
    monitors:
      upgrade: true
      migration: true
`
	path := writeTempConfig(t, yaml)

	cfg, err := daemon.LoadDaemonConfig(path)
	require.NoError(t, err)
	require.NotNil(t, cfg.Components.ConsensusNode)
	assert.Equal(t, daemon.CurrentSchemaVersion, cfg.SchemaVersion,
		"migrated config should carry the current schema version")
}

func TestLoadDaemonConfig_NewerVersionErrorIsErrConfigMalformed(t *testing.T) {
	// Confirm the error type is precisely ErrConfigMalformed (not a sub-type of
	// ErrConfig or any other namespace) so the doctor layer applies the right
	// exit-code mapping.
	yaml := "schema_version: 99\ncomponents: {}\n"
	path := writeTempConfig(t, yaml)

	_, err := daemon.LoadDaemonConfig(path)
	require.Error(t, err)

	ex := errorx.Cast(err)
	require.NotNil(t, ex, "error must be an errorx type for doctor.CheckErr to handle it")
	assert.True(t, strings.Contains(ex.Error(), "newer binary"))
}
