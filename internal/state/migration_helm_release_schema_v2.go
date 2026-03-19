// SPDX-License-Identifier: Apache-2.0

// migration_helm_release_schema_v2.go migrates the on-disk state.yaml from
// ModelVersion "v1" to "v2".
//
// v1 HelmReleaseInfo on-disk shape (written by ≤ v0.13.0):
//
//	blockNodeState:
//	  version:      <app version>   e.g. "v0.28.1"
//	  chartVersion: <chart version> e.g. "0.28.1"
//	  deleted:      <deletion time or "">
//
// v2 HelmReleaseInfo on-disk shape (from v0.14.0):
//
//	blockNodeState:
//	  version:    <chart version>  e.g. "0.28.1"
//	  appVersion: <app version>    e.g. "v0.28.1"
//	  deletedAt:  <deletion time or "">
//
// The migration operates entirely on raw yaml.Node trees — no typed Go struct
// deserialization — to avoid the chicken-and-egg problem where the new struct's
// field tags would silently drop the old keys before any remapping could occur.
//
// Key rename table (blockNodeState):
//
//	v1 key        → v2 key      carries
//	"version"     → "appVersion"  old app version value
//	"chartVersion"→ "version"     old chart version value
//	"deleted"     → "deletedAt"   old deletion timestamp value
//
// state.version is updated from "v1" to "v2" on Execute and restored on Rollback.

package state

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
)

// HelmReleaseSchemaV2Migration migrates the HelmReleaseInfo field layout from
// ModelVersion "v1" to "v2".
type HelmReleaseSchemaV2Migration struct {
	// stateFileOverride, when non-empty, is used as the state file path instead
	// of the production path derived from models.Paths().StateDir. Intended for
	// unit tests only.
	stateFileOverride string
}

// NewHelmReleaseSchemaV2Migration returns a new HelmReleaseSchemaV2Migration.
func NewHelmReleaseSchemaV2Migration() *HelmReleaseSchemaV2Migration {
	return &HelmReleaseSchemaV2Migration{}
}

func (m *HelmReleaseSchemaV2Migration) ID() string { return "helm-release-schema-v2" }
func (m *HelmReleaseSchemaV2Migration) Description() string {
	return "Migrate HelmReleaseInfo schema v1→v2: chartVersion→version, version→appVersion, deleted→deletedAt"
}

// stateFilePath returns the path of the state file this migration operates on.
// When stateFileOverride is set (tests), that path is returned directly;
// otherwise the production path under models.Paths().StateDir is used.
func (m *HelmReleaseSchemaV2Migration) stateFilePath() string {
	if m.stateFileOverride != "" {
		return m.stateFileOverride
	}
	return filepath.Join(models.Paths().StateDir, StateFileName)
}

// Applies returns true when the on-disk state file carries ModelVersion "v1".
func (m *HelmReleaseSchemaV2Migration) Applies(_ *migration.Context) (bool, error) {
	b, err := os.ReadFile(m.stateFilePath())
	if errors.Is(err, os.ErrNotExist) {
		return false, nil // fresh install — nothing to migrate
	}
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to read state file to check migration applicability")
	}
	return isV1StateFile(b), nil
}

// Execute transforms the on-disk state.yaml from v1 to v2 schema using raw
// yaml.Node manipulation and writes the result back atomically.
func (m *HelmReleaseSchemaV2Migration) Execute(_ context.Context, _ *migration.Context) error {
	stateFile := m.stateFilePath()

	b, err := os.ReadFile(stateFile)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to read state file for v1→v2 migration")
	}

	out, err := migrateStateV1ToV2(b)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to transform state YAML from v1 to v2")
	}

	if err := atomicWriteFile(stateFile, out); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write migrated state file")
	}
	return nil
}

// Rollback reverses the v1→v2 migration, restoring the v1 on-disk shape.
// Best-effort: intended for recovery scenarios only.
func (m *HelmReleaseSchemaV2Migration) Rollback(_ context.Context, _ *migration.Context) error {
	stateFile := m.stateFilePath()

	b, err := os.ReadFile(stateFile)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to read state file for v2→v1 rollback")
	}

	out, err := migrateStateV2ToV1(b)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to transform state YAML from v2 to v1")
	}

	if err := atomicWriteFile(stateFile, out); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write rolled-back state file")
	}
	return nil
}

// ── Pure transformation functions (no I/O — straightforward to unit test) ─────

// isV1StateFile reports whether raw YAML bytes represent a v1 state file by
// inspecting the state.version scalar without deserializing into Go structs.
func isV1StateFile(b []byte) bool {
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return false
	}
	stateNode := mappingValue(rootMappingNode(&doc), "state")
	if stateNode == nil {
		return false
	}
	return mappingScalar(stateNode, "version") == "v1"
}

// migrateStateV1ToV2 transforms raw YAML bytes from the v1 to the v2 schema.
// Pure function — no file I/O.
func migrateStateV1ToV2(b []byte) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}

	stateNode := mappingValue(rootMappingNode(&doc), "state")
	if stateNode == nil {
		return yaml.Marshal(&doc) // no "state" key — nothing to rename
	}

	bnNode := mappingValue(stateNode, "blockNodeState")
	if bnNode != nil {
		renameHelmReleaseFieldsV1ToV2(bnNode)
	}

	setMappingScalar(stateNode, "version", "v2")
	return yaml.Marshal(&doc)
}

// migrateStateV2ToV1 transforms raw YAML bytes from v2 back to v1 schema.
// Pure function — no file I/O. Used by Rollback.
func migrateStateV2ToV1(b []byte) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}

	stateNode := mappingValue(rootMappingNode(&doc), "state")
	if stateNode == nil {
		return yaml.Marshal(&doc)
	}

	bnNode := mappingValue(stateNode, "blockNodeState")
	if bnNode != nil {
		renameHelmReleaseFieldsV2ToV1(bnNode)
	}

	setMappingScalar(stateNode, "version", "v1")
	return yaml.Marshal(&doc)
}

// renameHelmReleaseFieldsV1ToV2 renames the three changed keys inside a
// blockNodeState mapping node from the v1 layout to v2.
//
// Rename order is deliberate: each step targets a distinct key name so that no
// rename step finds a key that was already renamed in a previous step:
//
//	step 1: "version"      → "appVersion"  (old app-version value moves to appVersion)
//	step 2: "chartVersion" → "version"     (old chart-version value becomes the new version)
//	step 3: "deleted"      → "deletedAt"
func renameHelmReleaseFieldsV1ToV2(node *yaml.Node) {
	if node.Kind != yaml.MappingNode {
		return
	}
	renameMappingKey(node, "version", "appVersion")
	renameMappingKey(node, "chartVersion", "version")
	renameMappingKey(node, "deleted", "deletedAt")
}

// renameHelmReleaseFieldsV2ToV1 is the exact inverse of renameHelmReleaseFieldsV1ToV2.
//
// Rename order matters: "version" (chart version) must be renamed to
// "chartVersion" BEFORE "appVersion" is renamed to "version". If the order
// were reversed, "appVersion" would be renamed to "version" first, creating
// two "version" keys; the next step would then pick up the newly-renamed key
// (app version) and wrongly label it "chartVersion".
//
//	step 1: "version"    → "chartVersion"  (restores chart version under v1 key)
//	step 2: "appVersion" → "version"       (restores app version under v1 key)
//	step 3: "deletedAt"  → "deleted"
func renameHelmReleaseFieldsV2ToV1(node *yaml.Node) {
	if node.Kind != yaml.MappingNode {
		return
	}
	renameMappingKey(node, "version", "chartVersion")
	renameMappingKey(node, "appVersion", "version")
	renameMappingKey(node, "deletedAt", "deleted")
}
