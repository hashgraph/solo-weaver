// SPDX-License-Identifier: Apache-2.0

// migration_traffic_shaping_enabled_v1.go replaces the traffic-shaping opt-out
// trafficShapingDisabled with an explicit trafficShapingEnabled.
//
// The old key was omitted when false, so a node with shaping on carried no key
// at all, and a missing key read as "enabled". The new key is always written,
// and a missing one reads as off.
//
// Old shapes:
//
//	blockNodeState:                      blockNodeState:
//	  shaping:                             trafficShapingDisabled: true
//	    egressInterface: eth0              shaping: {}
//
// New shapes:
//
//	blockNodeState:                      blockNodeState:
//	  trafficShapingEnabled: true          trafficShapingEnabled: false
//	  shaping:                             shaping: {}
//	    egressInterface: eth0
//
// With no opt-out, the shaping record decides: it is written only when shaping
// is enabled. A disabled node keeps its record for a later re-enable, so the
// opt-out is checked first.
//
// With neither, the node never recorded a decision. Installs from before the
// shaping record existed wrote no record when they opted in, so the action
// history's last block node install decides. Otherwise the node is off, but an
// older upgrade may have turned shaping on anyway, so it is marked with
// trafficShapingCleanupPending for the next upgrade to remove.
package state

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
)

const (
	trafficShapingEnabledKey        = "trafficShapingEnabled"
	trafficShapingDisabledKey       = "trafficShapingDisabled"
	trafficShapingCleanupPendingKey = "trafficShapingCleanupPending"
	actionHistoryFileName           = "action_history.yaml"
)

// TrafficShapingEnabledV1Migration rewrites blockNodeState.trafficShapingDisabled
// as an explicit trafficShapingEnabled.
type TrafficShapingEnabledV1Migration struct {
	// stateFileOverride, when non-empty, is used as the state file path instead
	// of the production path derived from models.Paths().StateDir. Intended for
	// unit tests only.
	stateFileOverride string
}

// NewTrafficShapingEnabledV1Migration returns a new TrafficShapingEnabledV1Migration.
func NewTrafficShapingEnabledV1Migration() *TrafficShapingEnabledV1Migration {
	return &TrafficShapingEnabledV1Migration{}
}

func (m *TrafficShapingEnabledV1Migration) ID() string { return "traffic-shaping-enabled-v1" }
func (m *TrafficShapingEnabledV1Migration) Description() string {
	return "Replace blockNodeState.trafficShapingDisabled with an explicit trafficShapingEnabled"
}

func (m *TrafficShapingEnabledV1Migration) stateFilePath() string {
	if m.stateFileOverride != "" {
		return m.stateFileOverride
	}
	return filepath.Join(models.Paths().StateDir, StateFileName)
}

// Applies returns true when blockNodeState lacks trafficShapingEnabled or still
// carries trafficShapingDisabled.
func (m *TrafficShapingEnabledV1Migration) Applies(_ *migration.Context) (bool, error) {
	b, err := os.ReadFile(m.stateFilePath())
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, errorx.ExternalError.Wrap(err, "failed to read state file to check migration applicability")
	}
	return needsTrafficShapingEnabled(b), nil
}

// Execute writes trafficShapingEnabled, removes trafficShapingDisabled, and
// writes the result back atomically.
func (m *TrafficShapingEnabledV1Migration) Execute(_ context.Context, _ *migration.Context) error {
	stateFile := m.stateFilePath()

	b, err := os.ReadFile(stateFile)
	if err != nil {
		return errorx.ExternalError.Wrap(err, "failed to read state file for trafficShapingEnabled migration")
	}

	history, err := os.ReadFile(filepath.Join(filepath.Dir(stateFile), actionHistoryFileName))
	if err != nil && !os.IsNotExist(err) {
		return errorx.ExternalError.Wrap(err, "failed to read action history for trafficShapingEnabled migration")
	}

	out, err := migrateToTrafficShapingEnabled(b, lastBlockNodeInstallOptedIn(history))
	if err != nil {
		return errorx.IllegalFormat.Wrap(err, "failed to rewrite traffic-shaping decision in state YAML")
	}

	if err := atomicWriteFile(stateFile, out); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to write migrated state file")
	}
	return nil
}

// Rollback restores the opt-out encoding: trafficShapingDisabled: true when
// shaping is off, no key when it is on.
func (m *TrafficShapingEnabledV1Migration) Rollback(_ context.Context, _ *migration.Context) error {
	stateFile := m.stateFilePath()

	b, err := os.ReadFile(stateFile)
	if err != nil {
		return errorx.ExternalError.Wrap(err, "failed to read state file for trafficShapingEnabled rollback")
	}

	out, err := migrateToTrafficShapingDisabled(b)
	if err != nil {
		return errorx.IllegalFormat.Wrap(err, "failed to restore trafficShapingDisabled in state YAML")
	}

	if err := atomicWriteFile(stateFile, out); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to write rolled-back state file")
	}
	return nil
}

// ── Pure transformation functions (no I/O — straightforward to unit test) ─────

func blockNodeStateNode(doc *yaml.Node) *yaml.Node {
	stateNode := mappingValue(rootMappingNode(doc), "state")
	return mappingValue(stateNode, "blockNodeState")
}

func needsTrafficShapingEnabled(b []byte) bool {
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return false
	}
	bn := blockNodeStateNode(&doc)
	if bn == nil || bn.Kind != yaml.MappingNode {
		return false
	}
	return mappingValue(bn, trafficShapingEnabledKey) == nil || mappingValue(bn, trafficShapingDisabledKey) != nil
}

// legacyTrafficShapingEnabled derives the decision from the opt-out encoding.
// decided is false when the state recorded neither an opt-out nor a shaping
// record.
func legacyTrafficShapingEnabled(bn *yaml.Node) (enabled, decided bool) {
	if mappingScalar(bn, trafficShapingDisabledKey) == "true" {
		return false, true
	}
	shaping := mappingValue(bn, "shaping")
	if shaping != nil && shaping.Tag != "!!null" {
		return true, true
	}
	return false, false
}

// lastBlockNodeInstallOptedIn reports whether the last block node install in
// the action history passed trafficshapingenabled: true. Entries that fail to
// parse are skipped: a full disk can cut one short.
func lastBlockNodeInstallOptedIn(history []byte) bool {
	optedIn := false
	for _, doc := range strings.Split(string(history), "\n---\n") {
		var entry struct {
			Intent struct {
				Action string `yaml:"action"`
				Target string `yaml:"target"`
			} `yaml:"intent"`
			Inputs struct {
				Custom struct {
					TrafficShapingEnabled bool `yaml:"trafficshapingenabled"`
				} `yaml:"custom"`
			} `yaml:"inputs"`
		}
		if yaml.Unmarshal([]byte(doc), &entry) != nil {
			continue
		}
		if entry.Intent.Action == "install" && entry.Intent.Target == "blocknode" {
			optedIn = entry.Inputs.Custom.TrafficShapingEnabled
		}
	}
	return optedIn
}

func migrateToTrafficShapingEnabled(b []byte, installOptedIn bool) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}

	bn := blockNodeStateNode(&doc)
	if bn != nil && bn.Kind == yaml.MappingNode {
		if mappingValue(bn, trafficShapingEnabledKey) == nil {
			enabled, decided := legacyTrafficShapingEnabled(bn)
			switch {
			case decided:
				appendMappingBool(bn, trafficShapingEnabledKey, enabled)
			case installOptedIn:
				appendMappingBool(bn, trafficShapingEnabledKey, true)
			default:
				appendMappingBool(bn, trafficShapingEnabledKey, false)
				appendMappingBool(bn, trafficShapingCleanupPendingKey, true)
			}
		}
		deleteMappingKey(bn, trafficShapingDisabledKey)
	}

	return yaml.Marshal(&doc)
}

func migrateToTrafficShapingDisabled(b []byte) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}

	bn := blockNodeStateNode(&doc)
	if bn != nil && bn.Kind == yaml.MappingNode {
		enabled := mappingScalar(bn, trafficShapingEnabledKey) == "true"
		deleteMappingKey(bn, trafficShapingEnabledKey)
		deleteMappingKey(bn, trafficShapingDisabledKey)
		deleteMappingKey(bn, trafficShapingCleanupPendingKey)
		if !enabled {
			appendMappingBool(bn, trafficShapingDisabledKey, true)
		}
	}

	return yaml.Marshal(&doc)
}

func appendMappingBool(node *yaml.Node, key string, val bool) {
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(val)},
	)
}

func deleteMappingKey(node *yaml.Node, key string) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content = append(node.Content[:i], node.Content[i+2:]...)
			return
		}
	}
}
