// SPDX-License-Identifier: Apache-2.0

package state

import (
	"maps"
	"slices"
	"strconv"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Equal returns true if two SoftwareState values are equal, ignoring LastSync.
func (s *SoftwareState) Equal(other SoftwareState) bool {
	if s.Name != other.Name ||
		s.Version != other.Version ||
		s.Installed != other.Installed ||
		s.Configured != other.Configured {
		return false
	}
	return models.IsEqualMap(s.Metadata, other.Metadata)
}

// Equal returns true if two HardwareState values are equal, ignoring LastSync.
func (s *HardwareState) Equal(other HardwareState) bool {
	if s.Type != other.Type ||
		s.Info != other.Info ||
		s.Count != other.Count ||
		s.Size != other.Size {
		return false
	}
	return models.IsEqualMap(s.Metadata, other.Metadata)
}

// Equal returns true if two MachineState values are equal, ignoring LastSync.
func (m *MachineState) Equal(other MachineState) bool {
	if len(m.Software) != len(other.Software) || len(m.Hardware) != len(other.Hardware) {
		return false
	}
	for name, sw := range m.Software {
		otherSw, ok := other.Software[name]
		if !ok || !sw.Equal(otherSw) {
			return false
		}
	}
	for name, hw := range m.Hardware {
		otherHw, ok := other.Hardware[name]
		if !ok || !hw.Equal(otherHw) {
			return false
		}
	}
	return true
}

// Equal returns true if two ClusterNodeState values are equal, ignoring LastSync.
func (c *ClusterNodeState) Equal(other ClusterNodeState) bool {
	if c.Name != other.Name ||
		c.Role != other.Role ||
		c.Ready != other.Ready ||
		c.KubeletVer != other.KubeletVer {
		return false
	}
	if len(c.Labels) != len(other.Labels) {
		return false
	}
	for k, v := range c.Labels {
		if other.Labels[k] != v {
			return false
		}
	}
	if len(c.Annotations) != len(other.Annotations) {
		return false
	}
	for k, v := range c.Annotations {
		if other.Annotations[k] != v {
			return false
		}
	}
	return true
}

// Equal returns true if two ClusterState values are equal, ignoring LastSync.
func (cs *ClusterState) Equal(other ClusterState) bool {
	return cs.Created == other.Created && cs.ClusterInfo.Equal(other.ClusterInfo)
}

// FieldDiff is one field whose persisted value differs from reality.
type FieldDiff struct {
	// Field is the field's key path in state.yaml, such as "storage.liveSize".
	Field     string
	Persisted string
	Reality   string
	// Observable is false for a weaver-only record, which reality cannot read
	// back, so a difference in it is not drift.
	Observable bool
}

// blockNodeField is one BlockNodeState field the diff compares. A nil same
// compares the two values as written.
type blockNodeField struct {
	name       string
	observable bool
	same       func(a, b string) bool
	value      func(*BlockNodeState) string
}

var blockNodeFields = []blockNodeField{
	{"name", true, nil, func(b *BlockNodeState) string { return b.ReleaseInfo.Name }},
	{"version", true, nil, func(b *BlockNodeState) string { return b.ReleaseInfo.ChartVersion }},
	{"appVersion", true, nil, func(b *BlockNodeState) string { return b.ReleaseInfo.AppVersion }},
	{"namespace", true, nil, func(b *BlockNodeState) string { return b.ReleaseInfo.Namespace }},
	{"chartRef", false, nil, func(b *BlockNodeState) string { return b.ReleaseInfo.ChartRef }},
	{"chartName", true, nil, func(b *BlockNodeState) string { return b.ReleaseInfo.ChartName }},
	{"status", true, nil, func(b *BlockNodeState) string { return string(b.ReleaseInfo.Status) }},
	{"storage.basePath", false, nil, func(b *BlockNodeState) string { return b.Storage.BasePath }},
	{"storage.archivePath", true, nil, func(b *BlockNodeState) string { return b.Storage.ArchivePath }},
	{"storage.livePath", true, nil, func(b *BlockNodeState) string { return b.Storage.LivePath }},
	{"storage.logPath", true, nil, func(b *BlockNodeState) string { return b.Storage.LogPath }},
	{"storage.verificationPath", true, nil, func(b *BlockNodeState) string { return b.Storage.VerificationPath }},
	{"storage.pluginsPath", true, nil, func(b *BlockNodeState) string { return b.Storage.PluginsPath }},
	{"storage.applicationStatePath", true, nil, func(b *BlockNodeState) string { return b.Storage.ApplicationStatePath }},
	{"storage.liveSize", true, sameQuantity, func(b *BlockNodeState) string { return b.Storage.LiveSize }},
	{"storage.archiveSize", true, sameQuantity, func(b *BlockNodeState) string { return b.Storage.ArchiveSize }},
	{"storage.logSize", true, sameQuantity, func(b *BlockNodeState) string { return b.Storage.LogSize }},
	{"storage.verificationSize", true, sameQuantity, func(b *BlockNodeState) string { return b.Storage.VerificationSize }},
	{"storage.pluginsSize", true, sameQuantity, func(b *BlockNodeState) string { return b.Storage.PluginsSize }},
	{"storage.applicationStateSize", true, sameQuantity, func(b *BlockNodeState) string { return b.Storage.ApplicationStateSize }},
	{"historicRetention", true, nil, func(b *BlockNodeState) string { return b.HistoricRetention }},
	{"recentRetention", true, nil, func(b *BlockNodeState) string { return b.RecentRetention }},
	{"pluginPreset", false, nil, func(b *BlockNodeState) string { return b.PluginPreset }},
	{"pluginList", false, nil, func(b *BlockNodeState) string { return b.PluginList }},
	{"trafficShapingDisabled", false, nil, func(b *BlockNodeState) string { return strconv.FormatBool(b.TrafficShapingDisabled) }},
	{"shaping.egressInterface", false, nil, func(b *BlockNodeState) string { return shapingOf(b).EgressInterface }},
	{"shaping.linkRate", false, nil, func(b *BlockNodeState) string { return shapingOf(b).LinkRate }},
}

// optionalField is an observable field that a state file written before it
// existed does not know; Diff compares it only when both sides know it.
type optionalField struct {
	name  string
	value func(*BlockNodeState) (value string, known bool)
}

var blockNodeOptionalFields = []optionalField{
	{"serviceTopology", func(b *BlockNodeState) (string, bool) { return b.ServiceTopology, b.ServiceTopology != "" }},
	{"metallbPool", func(b *BlockNodeState) (string, bool) { return formatOptionalBool(b.MetalLBPool), b.MetalLBPool != nil }},
}

func formatOptionalBool(v *bool) string {
	if v == nil {
		return ""
	}
	return strconv.FormatBool(*v)
}

// shapingOf returns b's shaping record, or an empty one when it was never set,
// so a nil record and an empty one compare the same.
func shapingOf(b *BlockNodeState) ShapingState {
	if b.Shaping == nil {
		return ShapingState{}
	}
	return *b.Shaping
}

// shapeOverrideFields returns the override fields of every traffic class either
// state records, sorted by class.
func shapeOverrideFields(a, b *BlockNodeState) []blockNodeField {
	classes := map[string]struct{}{}
	for _, s := range []*BlockNodeState{a, b} {
		for class := range shapingOf(s).ShapeOverrides {
			classes[class] = struct{}{}
		}
	}

	var fields []blockNodeField
	for _, class := range slices.Sorted(maps.Keys(classes)) {
		override := func(b *BlockNodeState) models.ShapeOverride { return shapingOf(b).ShapeOverrides[class] }
		prefix := "shaping.shapeOverrides." + class + "."
		fields = append(fields,
			blockNodeField{prefix + "rate", false, nil, func(b *BlockNodeState) string { return override(b).Rate }},
			blockNodeField{prefix + "ceil", false, nil, func(b *BlockNodeState) string { return override(b).Ceil }},
			blockNodeField{prefix + "prio", false, nil, func(b *BlockNodeState) string { return formatPrio(override(b).Prio) }},
		)
	}
	return fields
}

func formatPrio(prio *int) string {
	if prio == nil {
		return ""
	}
	return strconv.Itoa(*prio)
}

// sameQuantity compares two sizes as Kubernetes quantities, so 20Gi and 20480Mi
// match. A size that does not parse, including an empty one, is compared as written.
func sameQuantity(a, b string) bool {
	qa, errA := resource.ParseQuantity(a)
	qb, errB := resource.ParseQuantity(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return qa.Cmp(qb) == 0
}

// BlockNodeObservableFields returns the state.yaml keys of the BlockNodeState
// fields reality can read back, the ones Diff marks Observable.
func BlockNodeObservableFields() []string {
	var fields []string
	for _, f := range blockNodeFields {
		if f.observable {
			fields = append(fields, f.name)
		}
	}
	for _, f := range blockNodeOptionalFields {
		fields = append(fields, f.name)
	}
	return fields
}

// Diff returns the fields where the persisted state b differs from reality,
// ignoring the release timestamps and LastSync.
func (b *BlockNodeState) Diff(reality BlockNodeState) []FieldDiff {
	var diffs []FieldDiff
	for _, f := range slices.Concat(blockNodeFields, shapeOverrideFields(b, &reality)) {
		fromState, fromReality := f.value(b), f.value(&reality)
		same := fromState == fromReality
		if f.same != nil {
			same = f.same(fromState, fromReality)
		}
		if !same {
			diffs = append(diffs, FieldDiff{
				Field:      f.name,
				Persisted:  fromState,
				Reality:    fromReality,
				Observable: f.observable,
			})
		}
	}
	for _, f := range blockNodeOptionalFields {
		fromState, stateKnows := f.value(b)
		fromReality, realityKnows := f.value(&reality)
		if stateKnows && realityKnows && fromState != fromReality {
			diffs = append(diffs, FieldDiff{
				Field:      f.name,
				Persisted:  fromState,
				Reality:    fromReality,
				Observable: true,
			})
		}
	}
	return diffs
}
