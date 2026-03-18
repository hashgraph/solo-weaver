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

// v1StateYAML is a realistic state.yaml written by ≤ v0.13.0 with a block node
// that has been deployed. Key shape under blockNodeState:
//
//	version:      "v0.28.1"   (app version — old key)
//	chartVersion: "0.28.1"    (chart version — old key)
//	deleted:      ""           (deletion time — old key)
const v1StateYAML = `
hash: abc123
hashAlgo: sha256
stateFile: /opt/solo/weaver/state/state.yaml
state:
    version: v1
    blockNodeState:
        name: block-node
        version: v0.28.1
        namespace: block-node
        chartRef: oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server
        chartName: block-node-server
        chartVersion: "0.28.1"
        status: deployed
        deleted: ""
        storage: {}
`

// v1StateEmptyYAML is a v1 state.yaml where block node was never installed
// (all HelmReleaseInfo fields are zero/empty).
const v1StateEmptyYAML = `
hash: ""
hashAlgo: ""
stateFile: /opt/solo/weaver/state/state.yaml
state:
    version: v1
    blockNodeState:
        name: ""
        version: ""
        namespace: ""
        chartRef: ""
        chartName: ""
        chartVersion: ""
        status: unknown
        deleted: ""
        storage: {}
`

// v2StateYAML is the expected output after migrating v1StateYAML to v2.
// Used only to confirm the shape; exact byte comparison is not asserted.
const v2StateYAML = `
hash: abc123
hashAlgo: sha256
stateFile: /opt/solo/weaver/state/state.yaml
state:
    version: v2
    blockNodeState:
        name: block-node
        appVersion: v0.28.1
        namespace: block-node
        chartRef: oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server
        chartName: block-node-server
        version: "0.28.1"
        status: deployed
        deletedAt: ""
        storage: {}
`

// ── metadata ─────────────────────────────────────────────────────────────────

func TestHelmReleaseSchemaV2Migration_Metadata(t *testing.T) {
	m := NewHelmReleaseSchemaV2Migration()
	assert.Equal(t, "helm-release-schema-v2", m.ID())
	assert.Contains(t, m.Description(), "v1")
	assert.Contains(t, m.Description(), "v2")
}

// ── isV1StateFile ─────────────────────────────────────────────────────────────

func TestIsV1StateFile(t *testing.T) {
	t.Run("returns true for v1 file", func(t *testing.T) {
		assert.True(t, isV1StateFile([]byte(v1StateYAML)))
	})

	t.Run("returns false for v2 file", func(t *testing.T) {
		assert.False(t, isV1StateFile([]byte(v2StateYAML)))
	})

	t.Run("returns false for unparseable YAML", func(t *testing.T) {
		assert.False(t, isV1StateFile([]byte(":::not yaml:::")))
	})

	t.Run("returns false when state key is absent", func(t *testing.T) {
		assert.False(t, isV1StateFile([]byte("hash: abc\nstateFile: /tmp/x\n")))
	})

	t.Run("returns false for empty input", func(t *testing.T) {
		assert.False(t, isV1StateFile([]byte("")))
	})
}

// ── migrateStateV1ToV2 ───────────────────────────────────────────────────────

func TestMigrateStateV1ToV2_InstalledBlockNode(t *testing.T) {
	out, err := migrateStateV1ToV2([]byte(v1StateYAML))
	require.NoError(t, err)

	bn := parsedBlockNodeState(t, out)

	// v1 "chartVersion" value should now be under "version"
	assert.Equal(t, "0.28.1", mappingScalar(bn, "version"), "version should carry old chartVersion value")

	// v1 "version" value (app version) should now be under "appVersion"
	assert.Equal(t, "v0.28.1", mappingScalar(bn, "appVersion"), "appVersion should carry old version value")

	// v1 "deleted" key should be renamed to "deletedAt"
	assert.Equal(t, "", mappingScalar(bn, "deletedAt"), "deletedAt should be present")
	assert.Nil(t, mappingValue(bn, "deleted"), "old deleted key must be absent")

	// Old chartVersion key must be gone
	assert.Nil(t, mappingValue(bn, "chartVersion"), "old chartVersion key must be absent")

	// state.version must be v2
	assert.Equal(t, "v2", parsedStateVersion(t, out))
}

func TestMigrateStateV1ToV2_EmptyBlockNode(t *testing.T) {
	out, err := migrateStateV1ToV2([]byte(v1StateEmptyYAML))
	require.NoError(t, err)

	bn := parsedBlockNodeState(t, out)

	// All values are empty — keys are still renamed correctly
	assert.Equal(t, "", mappingScalar(bn, "version"), "version (chart) should be empty")
	assert.Equal(t, "", mappingScalar(bn, "appVersion"), "appVersion should be empty")
	assert.Equal(t, "", mappingScalar(bn, "deletedAt"), "deletedAt should be present and empty")
	assert.Nil(t, mappingValue(bn, "chartVersion"), "old chartVersion key must be absent")
	assert.Nil(t, mappingValue(bn, "deleted"), "old deleted key must be absent")

	assert.Equal(t, "v2", parsedStateVersion(t, out))
}

func TestMigrateStateV1ToV2_NoStateKey(t *testing.T) {
	input := []byte("hash: abc\nstateFile: /tmp/x\n")
	out, err := migrateStateV1ToV2(input)
	require.NoError(t, err)
	// No state key — YAML should be returned unchanged (minus marshal normalisation)
	assert.NotEmpty(t, out)
}

func TestMigrateStateV1ToV2_NoBlockNodeState(t *testing.T) {
	input := []byte("state:\n    version: v1\n    machineState: {}\n")
	out, err := migrateStateV1ToV2(input)
	require.NoError(t, err)
	// state.version should still be bumped even with no blockNodeState
	assert.Equal(t, "v2", parsedStateVersion(t, out))
}

func TestMigrateStateV1ToV2_InvalidYAML(t *testing.T) {
	// Invalid UTF-8 bytes are rejected by yaml.v3
	_, err := migrateStateV1ToV2([]byte{0x80, 0x81, 0x82})
	require.Error(t, err)
}

// ── migrateStateV2ToV1 (rollback) ────────────────────────────────────────────

func TestMigrateStateV2ToV1_InstalledBlockNode(t *testing.T) {
	out, err := migrateStateV2ToV1([]byte(v2StateYAML))
	require.NoError(t, err)

	bn := parsedBlockNodeState(t, out)

	// v2 "version" (chart version) should go back to "chartVersion"
	assert.Equal(t, "0.28.1", mappingScalar(bn, "chartVersion"), "chartVersion should be restored")

	// v2 "appVersion" should go back to "version"
	assert.Equal(t, "v0.28.1", mappingScalar(bn, "version"), "version should carry appVersion value")

	// v2 "deletedAt" should go back to "deleted"
	assert.Equal(t, "", mappingScalar(bn, "deleted"), "deleted should be restored")
	assert.Nil(t, mappingValue(bn, "deletedAt"), "deletedAt key must be absent after rollback")
	assert.Nil(t, mappingValue(bn, "appVersion"), "appVersion key must be absent after rollback")

	// state.version must be v1
	assert.Equal(t, "v1", parsedStateVersion(t, out))
}

// ── round-trip ────────────────────────────────────────────────────────────────

// TestMigrateRoundTrip verifies that migrating v1→v2→v1 preserves all field
// values. The test checks field values via yaml.Node rather than byte equality
// because yaml.Marshal may reformat whitespace/ordering.
func TestMigrateRoundTrip(t *testing.T) {
	// Forward
	v2, err := migrateStateV1ToV2([]byte(v1StateYAML))
	require.NoError(t, err)

	// Backward
	restored, err := migrateStateV2ToV1(v2)
	require.NoError(t, err)

	restoredBN := parsedBlockNodeState(t, restored)

	assert.Equal(t, "v0.28.1", mappingScalar(restoredBN, "version"), "app version must survive round-trip")
	assert.Equal(t, "0.28.1", mappingScalar(restoredBN, "chartVersion"), "chart version must survive round-trip")
	assert.Equal(t, "", mappingScalar(restoredBN, "deleted"), "deleted must survive round-trip")
	assert.Equal(t, "v1", parsedStateVersion(t, restored))
}

// ── yaml.Node helpers ─────────────────────────────────────────────────────────

func TestRootMappingNode(t *testing.T) {
	t.Run("returns root mapping from document", func(t *testing.T) {
		var doc yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte("key: value\n"), &doc))
		n := rootMappingNode(&doc)
		require.NotNil(t, n)
		assert.Equal(t, yaml.MappingNode, n.Kind)
	})

	t.Run("returns nil for nil input", func(t *testing.T) {
		assert.Nil(t, rootMappingNode(nil))
	})

	t.Run("returns nil for non-document node", func(t *testing.T) {
		n := &yaml.Node{Kind: yaml.MappingNode}
		assert.Nil(t, rootMappingNode(n))
	})

	t.Run("returns nil when document has no content", func(t *testing.T) {
		n := &yaml.Node{Kind: yaml.DocumentNode}
		assert.Nil(t, rootMappingNode(n))
	})
}

func TestMappingValue(t *testing.T) {
	node := buildMappingNode(t, "key: hello\nother: world\n")

	t.Run("finds existing key", func(t *testing.T) {
		v := mappingValue(node, "key")
		require.NotNil(t, v)
		assert.Equal(t, "hello", v.Value)
	})

	t.Run("returns nil for missing key", func(t *testing.T) {
		assert.Nil(t, mappingValue(node, "absent"))
	})

	t.Run("returns nil for nil node", func(t *testing.T) {
		assert.Nil(t, mappingValue(nil, "key"))
	})

	t.Run("returns nil for non-mapping node", func(t *testing.T) {
		scalar := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
		assert.Nil(t, mappingValue(scalar, "key"))
	})
}

func TestMappingScalar(t *testing.T) {
	node := buildMappingNode(t, "a: foo\nb: bar\n")

	assert.Equal(t, "foo", mappingScalar(node, "a"))
	assert.Equal(t, "bar", mappingScalar(node, "b"))
	assert.Equal(t, "", mappingScalar(node, "absent"))
	assert.Equal(t, "", mappingScalar(nil, "a"))
}

func TestSetMappingScalar(t *testing.T) {
	node := buildMappingNode(t, "x: old\n")

	t.Run("updates existing key", func(t *testing.T) {
		setMappingScalar(node, "x", "new")
		assert.Equal(t, "new", mappingScalar(node, "x"))
	})

	t.Run("no-op for missing key", func(t *testing.T) {
		setMappingScalar(node, "absent", "val") // must not panic
	})

	t.Run("no-op for nil node", func(t *testing.T) {
		setMappingScalar(nil, "x", "v") // must not panic
	})
}

func TestRenameMappingKey(t *testing.T) {
	t.Run("renames existing key, preserves value", func(t *testing.T) {
		node := buildMappingNode(t, "old: myvalue\n")
		renameMappingKey(node, "old", "new")
		assert.Nil(t, mappingValue(node, "old"), "old key must be gone")
		assert.Equal(t, "myvalue", mappingScalar(node, "new"))
	})

	t.Run("no-op for absent key", func(t *testing.T) {
		node := buildMappingNode(t, "a: 1\n")
		renameMappingKey(node, "absent", "b") // must not panic
		assert.Equal(t, "1", mappingScalar(node, "a"))
	})

	t.Run("renames only the first occurrence when key appears twice", func(t *testing.T) {
		// Construct a mapping with two keys sharing the same name (unusual but valid in Node API)
		node := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "dup"},
			{Kind: yaml.ScalarNode, Value: "first"},
			{Kind: yaml.ScalarNode, Value: "dup"},
			{Kind: yaml.ScalarNode, Value: "second"},
		}}
		renameMappingKey(node, "dup", "renamed")
		assert.Equal(t, "renamed", node.Content[0].Value)
		assert.Equal(t, "dup", node.Content[2].Value, "second occurrence must be untouched")
	})

	t.Run("no-op for nil node", func(t *testing.T) {
		renameMappingKey(nil, "a", "b") // must not panic
	})
}

// ── renameHelmReleaseFields ───────────────────────────────────────────────────

func TestRenameHelmReleaseFieldsV1ToV2(t *testing.T) {
	node := buildMappingNode(t, `
version: v0.28.1
chartVersion: "0.28.1"
deleted: "2026-01-01T00:00:00Z"
name: block-node
`)
	renameHelmReleaseFieldsV1ToV2(node)

	assert.Equal(t, "v0.28.1", mappingScalar(node, "appVersion"))
	assert.Equal(t, "0.28.1", mappingScalar(node, "version"))
	assert.Equal(t, "2026-01-01T00:00:00Z", mappingScalar(node, "deletedAt"))
	assert.Nil(t, mappingValue(node, "chartVersion"))
	assert.Nil(t, mappingValue(node, "deleted"))
	assert.Equal(t, "block-node", mappingScalar(node, "name"), "unrelated key untouched")
}

func TestRenameHelmReleaseFieldsV2ToV1(t *testing.T) {
	node := buildMappingNode(t, `
version: "0.28.1"
appVersion: v0.28.1
deletedAt: "2026-01-01T00:00:00Z"
name: block-node
`)
	renameHelmReleaseFieldsV2ToV1(node)

	assert.Equal(t, "v0.28.1", mappingScalar(node, "version"))
	assert.Equal(t, "0.28.1", mappingScalar(node, "chartVersion"))
	assert.Equal(t, "2026-01-01T00:00:00Z", mappingScalar(node, "deleted"))
	assert.Nil(t, mappingValue(node, "appVersion"))
	assert.Nil(t, mappingValue(node, "deletedAt"))
	assert.Equal(t, "block-node", mappingScalar(node, "name"), "unrelated key untouched")
}

func TestRenameHelmReleaseFieldsV1ToV2_NonMappingNodeIsNoOp(t *testing.T) {
	scalar := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
	renameHelmReleaseFieldsV1ToV2(scalar) // must not panic
}

// ── test helpers ──────────────────────────────────────────────────────────────

// buildMappingNode parses a YAML snippet and returns its root MappingNode.
func buildMappingNode(t *testing.T, yamlStr string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(yamlStr), &doc))
	n := rootMappingNode(&doc)
	require.NotNil(t, n)
	return n
}

// parsedBlockNodeState parses YAML bytes and returns the blockNodeState MappingNode.
func parsedBlockNodeState(t *testing.T, b []byte) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal(b, &doc))
	stateNode := mappingValue(rootMappingNode(&doc), "state")
	require.NotNil(t, stateNode, "state key must be present")
	bn := mappingValue(stateNode, "blockNodeState")
	require.NotNil(t, bn, "blockNodeState key must be present")
	return bn
}

// parsedStateVersion parses YAML bytes and returns the state.version scalar.
func parsedStateVersion(t *testing.T, b []byte) string {
	t.Helper()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal(b, &doc))
	stateNode := mappingValue(rootMappingNode(&doc), "state")
	require.NotNil(t, stateNode, "state key must be present")
	return mappingScalar(stateNode, "version")
}

// ── stateFilePath ─────────────────────────────────────────────────────────────

func TestStateFilePath_DefaultsToProductionPath(t *testing.T) {
	m := NewHelmReleaseSchemaV2Migration()
	// production path ends with StateFileName — we cannot assert the full path
	// because models.Paths() may vary, but the filename must always be present.
	assert.Contains(t, m.stateFilePath(), StateFileName)
}

func TestStateFilePath_OverrideIsUsed(t *testing.T) {
	m := &HelmReleaseSchemaV2Migration{stateFileOverride: "/tmp/custom-state.yaml"}
	assert.Equal(t, "/tmp/custom-state.yaml", m.stateFilePath())
}

// ── Applies ───────────────────────────────────────────────────────────────────

func TestApplies_FreshInstall_NoFile(t *testing.T) {
	dir := t.TempDir()
	m := &HelmReleaseSchemaV2Migration{
		stateFileOverride: filepath.Join(dir, "state.yaml"),
	}
	applies, err := m.Applies(nil)
	require.NoError(t, err)
	assert.False(t, applies, "no state file → migration must not apply")
}

func TestApplies_V1File(t *testing.T) {
	m := migrationWithTempState(t, v1StateYAML)
	applies, err := m.Applies(nil)
	require.NoError(t, err)
	assert.True(t, applies, "v1 file → migration must apply")
}

func TestApplies_V2File(t *testing.T) {
	m := migrationWithTempState(t, v2StateYAML)
	applies, err := m.Applies(nil)
	require.NoError(t, err)
	assert.False(t, applies, "v2 file → migration must not apply")
}

func TestApplies_UnreadableFile(t *testing.T) {
	// Point at a directory (not a regular file) to trigger a read error.
	dir := t.TempDir()
	m := &HelmReleaseSchemaV2Migration{stateFileOverride: dir}
	_, err := m.Applies(nil)
	require.Error(t, err, "unreadable path must return an error")
}

// ── Execute ───────────────────────────────────────────────────────────────────

func TestExecute_MigratesV1FileToV2(t *testing.T) {
	m := migrationWithTempState(t, v1StateYAML)

	require.NoError(t, m.Execute(context.Background(), nil))

	b, err := os.ReadFile(m.stateFilePath())
	require.NoError(t, err)
	assert.Equal(t, "v2", parsedStateVersion(t, b), "state.version must be v2 after Execute")

	bn := parsedBlockNodeState(t, b)
	assert.Equal(t, "0.28.1", mappingScalar(bn, "version"), "chart version under 'version'")
	assert.Equal(t, "v0.28.1", mappingScalar(bn, "appVersion"), "app version under 'appVersion'")
	assert.Nil(t, mappingValue(bn, "chartVersion"), "old chartVersion must be absent")
	assert.Nil(t, mappingValue(bn, "deleted"), "old deleted must be absent")
	assert.NotNil(t, mappingValue(bn, "deletedAt"), "deletedAt must be present")
}

func TestExecute_IdempotentOnV2File(t *testing.T) {
	// Executing on an already-v2 file should produce a valid v2 file (no error,
	// no data loss). The version bump is re-applied but the key renames are no-ops
	// because the v1 keys no longer exist.
	m := migrationWithTempState(t, v2StateYAML)

	require.NoError(t, m.Execute(context.Background(), nil))

	b, err := os.ReadFile(m.stateFilePath())
	require.NoError(t, err)
	assert.Equal(t, "v2", parsedStateVersion(t, b))
	// appVersion must still be present
	bn := parsedBlockNodeState(t, b)
	assert.Equal(t, "v0.28.1", mappingScalar(bn, "appVersion"))
}

func TestExecute_MissingFile_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	m := &HelmReleaseSchemaV2Migration{
		stateFileOverride: filepath.Join(dir, "missing.yaml"),
	}
	require.Error(t, m.Execute(context.Background(), nil))
}

// ── Rollback ──────────────────────────────────────────────────────────────────

func TestRollback_RestoresV1FromV2(t *testing.T) {
	m := migrationWithTempState(t, v2StateYAML)

	require.NoError(t, m.Rollback(context.Background(), nil))

	b, err := os.ReadFile(m.stateFilePath())
	require.NoError(t, err)
	assert.Equal(t, "v1", parsedStateVersion(t, b), "state.version must be v1 after Rollback")

	bn := parsedBlockNodeState(t, b)
	assert.Equal(t, "v0.28.1", mappingScalar(bn, "version"), "app version under old 'version' key")
	assert.Equal(t, "0.28.1", mappingScalar(bn, "chartVersion"), "chart version restored to 'chartVersion'")
	assert.Nil(t, mappingValue(bn, "appVersion"), "appVersion must be absent after rollback")
	assert.Nil(t, mappingValue(bn, "deletedAt"), "deletedAt must be absent after rollback")
	assert.NotNil(t, mappingValue(bn, "deleted"), "deleted key must be restored")
}

func TestRollback_ExecuteRollback_RoundTrip(t *testing.T) {
	m := migrationWithTempState(t, v1StateYAML)

	require.NoError(t, m.Execute(context.Background(), nil))
	require.NoError(t, m.Rollback(context.Background(), nil))

	b, err := os.ReadFile(m.stateFilePath())
	require.NoError(t, err)

	// After round-trip the file should be equivalent to the original v1 file.
	bn := parsedBlockNodeState(t, b)
	assert.Equal(t, "v1", parsedStateVersion(t, b))
	assert.Equal(t, "v0.28.1", mappingScalar(bn, "version"))
	assert.Equal(t, "0.28.1", mappingScalar(bn, "chartVersion"))
	assert.Equal(t, "", mappingScalar(bn, "deleted"))
}

func TestRollback_MissingFile_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	m := &HelmReleaseSchemaV2Migration{
		stateFileOverride: filepath.Join(dir, "missing.yaml"),
	}
	require.Error(t, m.Rollback(context.Background(), nil))
}

// ── helpers used only by the I/O tests ───────────────────────────────────────

// migrationWithTempState writes yamlContent to a temp file and returns a
// HelmReleaseSchemaV2Migration pointed at that file via stateFileOverride.
func migrationWithTempState(t *testing.T, yamlContent string) *HelmReleaseSchemaV2Migration {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yamlContent), 0o644))
	return &HelmReleaseSchemaV2Migration{stateFileOverride: path}
}

