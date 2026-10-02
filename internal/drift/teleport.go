// SPDX-License-Identifier: Apache-2.0

package drift

import (
	"strconv"

	"github.com/hashgraph/solo-weaver/internal/state"
)

const teleportComponent = "teleport"

type teleportField struct {
	name  string
	value func(state.TeleportState) string
}

// teleportAgent is one Teleport agent: its installed flag, and the fields that
// only mean something while it is installed.
type teleportAgent struct {
	installedField string
	installed      func(state.TeleportState) bool
	fields         []teleportField
}

var teleportNodeAgent = teleportAgent{
	installedField: "nodeAgent.installed",
	installed:      func(t state.TeleportState) bool { return t.NodeAgent.Installed },
	// nodeAgent.version is not compared: the checker reports the version this
	// binary would install, not the one on the host, so a provisioner upgrade
	// alone would change it.
	fields: []teleportField{
		{"nodeAgent.configured", func(t state.TeleportState) string { return strconv.FormatBool(t.NodeAgent.Configured) }},
	},
}

var teleportClusterAgent = teleportAgent{
	installedField: "clusterAgent.installed",
	installed:      func(t state.TeleportState) bool { return t.ClusterAgent.Installed },
	fields: []teleportField{
		{"clusterAgent.release", func(t state.TeleportState) string { return t.ClusterAgent.Release }},
		{"clusterAgent.namespace", func(t state.TeleportState) string { return t.ClusterAgent.Namespace }},
		{"clusterAgent.chartVersion", func(t state.TeleportState) string { return t.ClusterAgent.ChartVersion }},
	},
}

// Teleport reports Teleport node and cluster agent fields that differ between
// state.yaml and the host or cluster. It relies on the Teleport checker keeping
// the persisted agent when a probe fails, so a difference is always an observed one.
func Teleport(baseline, live state.State) []Change {
	return append(
		teleportNodeAgent.compare(baseline.TeleportState, live.TeleportState),
		teleportClusterAgent.compare(baseline.TeleportState, live.TeleportState)...)
}

// compare reports an install or removal as the installed flag alone, since the
// agent's other fields follow it, and compares those fields only while the
// agent is installed on both sides.
func (a teleportAgent) compare(persisted, observed state.TeleportState) []Change {
	was, is := a.installed(persisted), a.installed(observed)
	if was != is {
		return []Change{{
			Component: teleportComponent,
			Field:     a.installedField,
			Persisted: strconv.FormatBool(was),
			Live:      strconv.FormatBool(is),
		}}
	}
	if !is {
		return nil
	}

	var changes []Change
	for _, f := range a.fields {
		if p, o := f.value(persisted), f.value(observed); p != o {
			changes = append(changes, Change{Component: teleportComponent, Field: f.name, Persisted: p, Live: o})
		}
	}
	return changes
}
