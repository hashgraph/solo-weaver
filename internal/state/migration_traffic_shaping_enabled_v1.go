// SPDX-License-Identifier: Apache-2.0

// migration_traffic_shaping_enabled_v1.go records an explicit
// trafficShapingEnabled: true on block nodes that already run traffic shaping.
//
// The decision used to be stored only as the opt-out trafficShapingDisabled
// (omitted when false), so a node with shaping on carried no key at all. Now
// a missing trafficShapingEnabled reads as off, and without this migration
// those nodes would lose shaping on the next reconfigure.
//
// Old shape (shaping on):
//
//	state:
//	  blockNodeState:
//	    shaping:
//	      egressInterface: eth0
//
// New shape:
//
//	state:
//	  blockNodeState:
//	    trafficShapingEnabled: true
//	    shaping:
//	      egressInterface: eth0
//
// The shaping record is written only when shaping is enabled, so it marks the
// nodes to migrate. A node with trafficShapingDisabled: true keeps its record
// for a later re-enable and is left alone.
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

const (
	trafficShapingEnabledKey  = "trafficShapingEnabled"
	trafficShapingDisabledKey = "trafficShapingDisabled"
)

// TrafficShapingEnabledV1Migration writes trafficShapingEnabled: true for
// block nodes whose state shows shaping on under the old opt-out encoding.
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
	return "Record trafficShapingEnabled: true for block nodes that run traffic shaping without an explicit opt-out"
}

func (m *TrafficShapingEnabledV1Migration) stateFilePath() string {
	if m.stateFileOverride != "" {
		return m.stateFileOverride
	}
	return filepath.Join(models.Paths().StateDir, StateFileName)
}

// Applies returns true when the on-disk state shows shaping on under the old
// encoding and has no trafficShapingEnabled key yet.
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

// Execute adds trafficShapingEnabled: true to blockNodeState and writes the
// result back atomically.
func (m *TrafficShapingEnabledV1Migration) Execute(_ context.Context, _ *migration.Context) error {
	stateFile := m.stateFilePath()

	b, err := os.ReadFile(stateFile)
	if err != nil {
		return errorx.ExternalError.Wrap(err, "failed to read state file for trafficShapingEnabled migration")
	}

	out, err := addTrafficShapingEnabled(b)
	if err != nil {
		return errorx.IllegalFormat.Wrap(err, "failed to add trafficShapingEnabled to state YAML")
	}

	if err := atomicWriteFile(stateFile, out); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to write migrated state file")
	}
	return nil
}

// Rollback removes trafficShapingEnabled. Older CLIs never read it, so the
// file is back to the shape they wrote.
func (m *TrafficShapingEnabledV1Migration) Rollback(_ context.Context, _ *migration.Context) error {
	stateFile := m.stateFilePath()

	b, err := os.ReadFile(stateFile)
	if err != nil {
		return errorx.ExternalError.Wrap(err, "failed to read state file for trafficShapingEnabled rollback")
	}

	out, err := removeTrafficShapingEnabled(b)
	if err != nil {
		return errorx.IllegalFormat.Wrap(err, "failed to remove trafficShapingEnabled from state YAML")
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

// shapingOnUnderOptOut reports whether bn has a shaping record, no opt-out,
// and no trafficShapingEnabled key.
func shapingOnUnderOptOut(bn *yaml.Node) bool {
	if bn == nil || mappingValue(bn, trafficShapingEnabledKey) != nil {
		return false
	}
	if mappingScalar(bn, trafficShapingDisabledKey) == "true" {
		return false
	}
	shaping := mappingValue(bn, "shaping")
	return shaping != nil && shaping.Tag != "!!null"
}

func needsTrafficShapingEnabled(b []byte) bool {
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return false
	}
	return shapingOnUnderOptOut(blockNodeStateNode(&doc))
}

// addTrafficShapingEnabled returns b with trafficShapingEnabled: true added to
// blockNodeState when shapingOnUnderOptOut holds, and unchanged otherwise.
func addTrafficShapingEnabled(b []byte) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}

	bn := blockNodeStateNode(&doc)
	if shapingOnUnderOptOut(bn) {
		bn.Content = append(bn.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: trafficShapingEnabledKey},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"},
		)
	}

	return yaml.Marshal(&doc)
}

func removeTrafficShapingEnabled(b []byte) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}

	bn := blockNodeStateNode(&doc)
	if bn != nil && bn.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(bn.Content); i += 2 {
			if bn.Content[i].Value == trafficShapingEnabledKey {
				bn.Content = append(bn.Content[:i], bn.Content[i+2:]...)
				break
			}
		}
	}

	return yaml.Marshal(&doc)
}
