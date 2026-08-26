// SPDX-License-Identifier: Apache-2.0

// migration_mgmt_ports_v1.go carries an already-provisioned host's persisted
// management port forward when the `mgmtPorts` list replaces the old
// single-value `sshPort` field on state.machineState.firewall (see issue
// #1080, which also renamed the CLI flag from --ssh-port to --mgmt-ports).
//
// Old shape (written by ≤ the pre-#1080 CLI):
//
//	state:
//	  machineState:
//	    firewall:
//	      sshPort: 2222
//
// New shape:
//
//	state:
//	  machineState:
//	    firewall:
//	      mgmtPorts:
//	        - 2222
//
// This targets only the machineState.firewall node — unlike
// migration_helm_release_schema_v2.go, there is no whole-document schema
// version to bump for a single-field rename.

package state

import (
	"context"
	"os"
	"path/filepath"

	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
)

// MgmtPortsV1Migration renames state.machineState.firewall.sshPort (a scalar)
// to mgmtPorts (a one-element list), preserving the operator's configured
// port across the upgrade.
type MgmtPortsV1Migration struct {
	// stateFileOverride, when non-empty, is used as the state file path instead
	// of the production path derived from models.Paths().StateDir. Intended for
	// unit tests only.
	stateFileOverride string
}

// NewMgmtPortsV1Migration returns a new MgmtPortsV1Migration.
func NewMgmtPortsV1Migration() *MgmtPortsV1Migration {
	return &MgmtPortsV1Migration{}
}

func (m *MgmtPortsV1Migration) ID() string { return "mgmt-ports-v1" }
func (m *MgmtPortsV1Migration) Description() string {
	return "Carry forward machineState.firewall.sshPort (scalar) as mgmtPorts (list) on already-provisioned hosts"
}

// stateFilePath returns the path of the state file this migration operates on.
func (m *MgmtPortsV1Migration) stateFilePath() string {
	if m.stateFileOverride != "" {
		return m.stateFileOverride
	}
	return filepath.Join(models.Paths().StateDir, StateFileName)
}

// Applies returns true when the on-disk state file still carries the legacy
// machineState.firewall.sshPort scalar and has not yet been migrated to
// mgmtPorts.
func (m *MgmtPortsV1Migration) Applies(_ *migration.Context) (bool, error) {
	b, err := os.ReadFile(m.stateFilePath())
	if os.IsNotExist(err) {
		return false, nil // fresh install — nothing to migrate
	}
	if err != nil {
		return false, errorx.ExternalError.Wrap(err, "failed to read state file to check migration applicability")
	}
	return hasLegacySSHPort(b), nil
}

// Execute rewrites the on-disk state.yaml, moving sshPort's value into a
// one-element mgmtPorts list, and writes the result back atomically.
func (m *MgmtPortsV1Migration) Execute(_ context.Context, _ *migration.Context) error {
	stateFile := m.stateFilePath()

	b, err := os.ReadFile(stateFile)
	if err != nil {
		return errorx.ExternalError.Wrap(err, "failed to read state file for sshPort→mgmtPorts migration")
	}

	out, err := migrateSSHPortToMgmtPorts(b)
	if err != nil {
		return errorx.IllegalFormat.Wrap(err, "failed to transform state YAML from sshPort to mgmtPorts")
	}

	if err := atomicWriteFile(stateFile, out); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to write migrated state file")
	}
	return nil
}

// Rollback reverses the migration, restoring sshPort from the first element of
// mgmtPorts. Best-effort: if mgmtPorts has more than one element, the rest are
// dropped, matching the "may not fully restore" rollback contract.
func (m *MgmtPortsV1Migration) Rollback(_ context.Context, _ *migration.Context) error {
	stateFile := m.stateFilePath()

	b, err := os.ReadFile(stateFile)
	if err != nil {
		return errorx.ExternalError.Wrap(err, "failed to read state file for mgmtPorts→sshPort rollback")
	}

	out, err := migrateMgmtPortsToSSHPort(b)
	if err != nil {
		return errorx.IllegalFormat.Wrap(err, "failed to transform state YAML from mgmtPorts to sshPort")
	}

	if err := atomicWriteFile(stateFile, out); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to write rolled-back state file")
	}
	return nil
}

// ── Pure transformation functions (no I/O — straightforward to unit test) ─────

// firewallNode walks state.machineState.firewall inside a parsed document,
// returning nil if any segment of the path is absent.
func firewallNode(doc *yaml.Node) *yaml.Node {
	stateNode := mappingValue(rootMappingNode(doc), "state")
	machineStateNode := mappingValue(stateNode, "machineState")
	return mappingValue(machineStateNode, "firewall")
}

// hasLegacySSHPort reports whether raw YAML bytes carry
// machineState.firewall.sshPort without an mgmtPorts sibling, by inspecting
// the node tree without deserializing into Go structs.
func hasLegacySSHPort(b []byte) bool {
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return false
	}
	fw := firewallNode(&doc)
	if fw == nil {
		return false
	}
	return mappingValue(fw, "sshPort") != nil && mappingValue(fw, "mgmtPorts") == nil
}

// migrateSSHPortToMgmtPorts transforms raw YAML bytes, renaming
// machineState.firewall.sshPort to mgmtPorts and wrapping its scalar value in
// a one-element sequence. Pure function — no file I/O. A document with no
// firewall.sshPort is returned unchanged.
func migrateSSHPortToMgmtPorts(b []byte) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}

	fw := firewallNode(&doc)
	if fw == nil {
		return yaml.Marshal(&doc)
	}

	wrapMappingValueAsSequence(fw, "sshPort")
	renameMappingKey(fw, "sshPort", "mgmtPorts")

	return yaml.Marshal(&doc)
}

// migrateMgmtPortsToSSHPort is the best-effort inverse of
// migrateSSHPortToMgmtPorts, used by Rollback. Only the first element of
// mgmtPorts survives; any additional ports the operator added after Execute
// ran are dropped.
func migrateMgmtPortsToSSHPort(b []byte) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}

	fw := firewallNode(&doc)
	if fw == nil {
		return yaml.Marshal(&doc)
	}

	unwrapMappingValueFromSequence(fw, "mgmtPorts")
	renameMappingKey(fw, "mgmtPorts", "sshPort")

	return yaml.Marshal(&doc)
}

// wrapMappingValueAsSequence replaces the value of key inside a MappingNode
// with a one-element SequenceNode holding the original scalar value. No-op if
// key is absent or its value is not a scalar.
func wrapMappingValueAsSequence(node *yaml.Node, key string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value != key {
			continue
		}
		val := node.Content[i+1]
		if val.Kind != yaml.ScalarNode {
			return
		}
		node.Content[i+1] = &yaml.Node{
			Kind:    yaml.SequenceNode,
			Content: []*yaml.Node{val},
		}
		return
	}
}

// unwrapMappingValueFromSequence replaces the value of key inside a
// MappingNode with the first element of its SequenceNode value. No-op if key
// is absent, its value is not a sequence, or the sequence is empty.
func unwrapMappingValueFromSequence(node *yaml.Node, key string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value != key {
			continue
		}
		val := node.Content[i+1]
		if val.Kind != yaml.SequenceNode || len(val.Content) == 0 {
			return
		}
		node.Content[i+1] = val.Content[0]
		return
	}
}
