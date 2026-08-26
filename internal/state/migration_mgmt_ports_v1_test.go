// SPDX-License-Identifier: Apache-2.0

package state

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// ── fixtures ─────────────────────────────────────────────────────────────────

// legacySSHPortYAML is a realistic state.yaml written by the pre-#1080 CLI,
// carrying the single-value sshPort scalar.
const legacySSHPortYAML = `
hash: abc123
hashAlgo: sha256
stateFile: /opt/solo/weaver/state/state.yaml
state:
    version: v2
    machineState:
        firewall:
            disabled: false
            managementCidrs:
                - 10.0.0.0/8
            sshPort: 2222
            podCidr: 10.4.0.0/14
`

// mgmtPortsYAML is the expected shape after migrating legacySSHPortYAML.
const mgmtPortsYAML = `
hash: abc123
hashAlgo: sha256
stateFile: /opt/solo/weaver/state/state.yaml
state:
    version: v2
    machineState:
        firewall:
            disabled: false
            managementCidrs:
                - 10.0.0.0/8
            mgmtPorts:
                - 2222
            podCidr: 10.4.0.0/14
`

// ── metadata ─────────────────────────────────────────────────────────────────

func TestMgmtPortsV1Migration_Metadata(t *testing.T) {
	m := NewMgmtPortsV1Migration()
	assert.Equal(t, "mgmt-ports-v1", m.ID())
	assert.Contains(t, m.Description(), "sshPort")
	assert.Contains(t, m.Description(), "mgmtPorts")
}

// ── stateFilePath ─────────────────────────────────────────────────────────────

func TestMgmtPortsV1_StateFilePath_DefaultsToProductionPath(t *testing.T) {
	m := NewMgmtPortsV1Migration()
	assert.Contains(t, m.stateFilePath(), StateFileName)
}

func TestMgmtPortsV1_StateFilePath_OverrideIsUsed(t *testing.T) {
	m := &MgmtPortsV1Migration{stateFileOverride: "/tmp/custom-state.yaml"}
	assert.Equal(t, "/tmp/custom-state.yaml", m.stateFilePath())
}

// ── hasLegacySSHPort ───────────────────────────────────────────────────────────

func TestHasLegacySSHPort(t *testing.T) {
	t.Run("returns true when sshPort present without mgmtPorts", func(t *testing.T) {
		assert.True(t, hasLegacySSHPort([]byte(legacySSHPortYAML)))
	})

	t.Run("returns false once migrated to mgmtPorts", func(t *testing.T) {
		assert.False(t, hasLegacySSHPort([]byte(mgmtPortsYAML)))
	})

	t.Run("returns false when firewall is absent", func(t *testing.T) {
		assert.False(t, hasLegacySSHPort([]byte("state:\n    machineState: {}\n")))
	})

	t.Run("returns false for unparseable YAML", func(t *testing.T) {
		assert.False(t, hasLegacySSHPort([]byte(":::not yaml:::")))
	})

	t.Run("returns false for empty input", func(t *testing.T) {
		assert.False(t, hasLegacySSHPort([]byte("")))
	})
}

// ── migrateSSHPortToMgmtPorts ─────────────────────────────────────────────────

func TestMigrateSSHPortToMgmtPorts(t *testing.T) {
	out, err := migrateSSHPortToMgmtPorts([]byte(legacySSHPortYAML))
	require.NoError(t, err)

	fw := parsedFirewallNode(t, out)
	require.Nil(t, mappingValue(fw, "sshPort"), "sshPort key must be removed")

	ports := mappingValue(fw, "mgmtPorts")
	require.NotNil(t, ports, "mgmtPorts key must be present")
	require.Equal(t, yaml.SequenceNode, ports.Kind)
	require.Len(t, ports.Content, 1)
	assert.Equal(t, "2222", ports.Content[0].Value)

	// Sibling fields must be untouched.
	assert.Equal(t, "10.4.0.0/14", mappingScalar(fw, "podCidr"))
}

func TestMigrateSSHPortToMgmtPorts_NoFirewallIsNoOp(t *testing.T) {
	in := []byte("state:\n    machineState:\n        profile: mainnet\n")
	out, err := migrateSSHPortToMgmtPorts(in)
	require.NoError(t, err)

	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal(out, &doc))
	stateNode := mappingValue(rootMappingNode(&doc), "state")
	machineStateNode := mappingValue(stateNode, "machineState")
	assert.Equal(t, "mainnet", mappingScalar(machineStateNode, "profile"))
}

// ── migrateMgmtPortsToSSHPort (Rollback) ──────────────────────────────────────

func TestMigrateMgmtPortsToSSHPort(t *testing.T) {
	out, err := migrateMgmtPortsToSSHPort([]byte(mgmtPortsYAML))
	require.NoError(t, err)

	fw := parsedFirewallNode(t, out)
	require.Nil(t, mappingValue(fw, "mgmtPorts"), "mgmtPorts key must be removed")
	assert.Equal(t, "2222", mappingScalar(fw, "sshPort"))
}

func TestMigrateMgmtPortsToSSHPort_MultiplePortsKeepsOnlyFirst(t *testing.T) {
	in := []byte(`
state:
    machineState:
        firewall:
            mgmtPorts:
                - 22
                - 2222
`)
	out, err := migrateMgmtPortsToSSHPort(in)
	require.NoError(t, err)

	fw := parsedFirewallNode(t, out)
	assert.Equal(t, "22", mappingScalar(fw, "sshPort"), "rollback is best-effort: only the first port survives")
}

// ── round trip ────────────────────────────────────────────────────────────────

func TestMgmtPortsV1_RoundTrip(t *testing.T) {
	migrated, err := migrateSSHPortToMgmtPorts([]byte(legacySSHPortYAML))
	require.NoError(t, err)
	assert.False(t, hasLegacySSHPort(migrated))

	rolledBack, err := migrateMgmtPortsToSSHPort(migrated)
	require.NoError(t, err)
	assert.True(t, hasLegacySSHPort(rolledBack))
}

// ── Applies / Execute / Rollback (file I/O) ───────────────────────────────────

func TestMgmtPortsV1_Applies_FreshInstall_NoFile(t *testing.T) {
	dir := t.TempDir()
	m := &MgmtPortsV1Migration{stateFileOverride: filepath.Join(dir, "state.yaml")}
	applies, err := m.Applies(nil)
	require.NoError(t, err)
	assert.False(t, applies)
}

func TestMgmtPortsV1_Applies_LegacyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.yaml")
	require.NoError(t, os.WriteFile(path, []byte(legacySSHPortYAML), 0o600))
	m := &MgmtPortsV1Migration{stateFileOverride: path}
	applies, err := m.Applies(nil)
	require.NoError(t, err)
	assert.True(t, applies)
}

func TestMgmtPortsV1_Applies_AlreadyMigrated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.yaml")
	require.NoError(t, os.WriteFile(path, []byte(mgmtPortsYAML), 0o600))
	m := &MgmtPortsV1Migration{stateFileOverride: path}
	applies, err := m.Applies(nil)
	require.NoError(t, err)
	assert.False(t, applies)
}

func TestMgmtPortsV1_ExecuteThenRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.yaml")
	require.NoError(t, os.WriteFile(path, []byte(legacySSHPortYAML), 0o600))
	m := &MgmtPortsV1Migration{stateFileOverride: path}

	applies, err := m.Applies(nil)
	require.NoError(t, err)
	require.True(t, applies)

	require.NoError(t, m.Execute(context.Background(), nil))

	applies, err = m.Applies(nil)
	require.NoError(t, err)
	assert.False(t, applies, "must not re-apply after Execute")

	b, err := os.ReadFile(path)
	require.NoError(t, err)
	fw := parsedFirewallNode(t, b)
	ports := mappingValue(fw, "mgmtPorts")
	require.NotNil(t, ports)
	require.Len(t, ports.Content, 1)
	assert.Equal(t, "2222", ports.Content[0].Value)

	require.NoError(t, m.Rollback(context.Background(), nil))

	applies, err = m.Applies(nil)
	require.NoError(t, err)
	assert.True(t, applies, "must apply again after Rollback restores the legacy shape")
}

// ── test helpers ──────────────────────────────────────────────────────────────

// parsedFirewallNode parses YAML bytes and returns the
// state.machineState.firewall MappingNode.
func parsedFirewallNode(t *testing.T, b []byte) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal(b, &doc))
	fw := firewallNode(&doc)
	require.NotNil(t, fw, "machineState.firewall must be present")
	return fw
}
