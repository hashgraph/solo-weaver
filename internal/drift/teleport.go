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
	// removedWhenFalse marks a boolean field whose going from true to false is
	// a removal.
	removedWhenFalse bool
}

// teleportAgent is one Teleport agent: its installed flag, whether this run
// could read it, and the fields that only mean something while it is installed.
type teleportAgent struct {
	installedField string
	installed      func(state.TeleportState) bool
	unobserved     func(state.TeleportState) bool
	fields         []teleportField
}

var teleportNodeAgent = teleportAgent{
	installedField: "nodeAgent.installed",
	installed:      func(t state.TeleportState) bool { return t.NodeAgent.Installed },
	unobserved:     func(t state.TeleportState) bool { return t.NodeAgent.Unobserved },
	// nodeAgent.version is not compared: the checker reports the version this
	// binary would install, not the one on the host, so a provisioner upgrade
	// alone would change it.
	fields: []teleportField{
		// configured only says the agent's /usr/local/bin links point into the
		// sandbox, so its going false is a removal too.
		{name: "nodeAgent.configured", value: func(t state.TeleportState) string { return strconv.FormatBool(t.NodeAgent.Configured) }, removedWhenFalse: true},
	},
}

var teleportClusterAgent = teleportAgent{
	installedField: "clusterAgent.installed",
	installed:      func(t state.TeleportState) bool { return t.ClusterAgent.Installed },
	unobserved:     func(t state.TeleportState) bool { return t.ClusterAgent.Unobserved },
	fields: []teleportField{
		{name: "clusterAgent.release", value: func(t state.TeleportState) string { return t.ClusterAgent.Release }},
		{name: "clusterAgent.namespace", value: func(t state.TeleportState) string { return t.ClusterAgent.Namespace }},
		{name: "clusterAgent.chartVersion", value: func(t state.TeleportState) string { return t.ClusterAgent.ChartVersion }},
	},
}

// Teleport reports Teleport node and cluster agent fields that differ between
// state.yaml and the host or cluster. An agent the Teleport checker could not
// read is reported once, as an Unobserved change on its installed flag.
func Teleport(baseline, live state.State) []Change {
	return append(
		teleportNodeAgent.compare(baseline.TeleportState, live.TeleportState),
		teleportClusterAgent.compare(baseline.TeleportState, live.TeleportState)...)
}

// OnTeleportNodeAgent reports whether c is a change to the Teleport node agent.
func OnTeleportNodeAgent(c Change) bool { return teleportNodeAgent.owns(c) }

// OnTeleportClusterAgent reports whether c is a change to the Teleport cluster
// agent.
func OnTeleportClusterAgent(c Change) bool { return teleportClusterAgent.owns(c) }

func (a teleportAgent) owns(c Change) bool {
	if c.Component != teleportComponent {
		return false
	}
	if c.Field == a.installedField {
		return true
	}
	for _, f := range a.fields {
		if f.name == c.Field {
			return true
		}
	}
	return false
}

// compare reports an install or removal as the installed flag alone, since the
// agent's other fields follow it, and compares those fields only while the
// agent is installed on both sides. An agent it could not read is reported on
// its installed flag too, even one recorded as not installed: one may have
// been installed by hand since.
func (a teleportAgent) compare(persisted, observed state.TeleportState) []Change {
	was, is := a.installed(persisted), a.installed(observed)
	if a.unobserved(observed) {
		return []Change{{
			Component:  teleportComponent,
			Field:      a.installedField,
			Persisted:  strconv.FormatBool(was),
			Unobserved: true,
		}}
	}
	if was != is {
		return []Change{{
			Component: teleportComponent,
			Field:     a.installedField,
			Persisted: strconv.FormatBool(was),
			Live:      strconv.FormatBool(is),
			Removed:   was,
		}}
	}
	if !is {
		return nil
	}

	var changes []Change
	for _, f := range a.fields {
		if p, o := f.value(persisted), f.value(observed); p != o {
			changes = append(changes, Change{
				Component: teleportComponent, Field: f.name, Persisted: p, Live: o,
				Removed: f.removedWhenFalse && p == "true",
			})
		}
	}
	return changes
}
