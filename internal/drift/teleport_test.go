// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package drift

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/stretchr/testify/require"
)

func teleportInstalled() state.TeleportState {
	return state.TeleportState{
		NodeAgent: state.TeleportNodeAgentState{Installed: true, Configured: true, Version: "18.1.0"},
		ClusterAgent: state.TeleportClusterAgentState{
			Installed: true, Release: "teleport-agent", Namespace: "teleport-agent", ChartVersion: "18.1.0",
		},
	}
}

// withTeleport returns a State holding t, as seen by a refresh that reached the cluster.
func withTeleport(t state.TeleportState) state.State {
	s := state.State{}
	s.TeleportState = t
	s.ClusterState.Created = true
	return s
}

func TestTeleport_SameStateHasNoChanges(t *testing.T) {
	require.Empty(t, Teleport(withTeleport(teleportInstalled()), withTeleport(teleportInstalled())))
	require.Empty(t, Teleport(withTeleport(state.TeleportState{}), withTeleport(state.TeleportState{})))
}

func TestTeleport_ReportsEachChangedFieldSeparately(t *testing.T) {
	live := teleportInstalled()
	live.NodeAgent.Configured = false
	live.ClusterAgent.ChartVersion = "18.2.0"
	live.ClusterAgent.Namespace = "teleport"

	got := Teleport(withTeleport(teleportInstalled()), withTeleport(live))

	require.Equal(t, []Change{
		{Component: "teleport", Field: "nodeAgent.configured", Persisted: "true", Live: "false"},
		{Component: "teleport", Field: "clusterAgent.namespace", Persisted: "teleport-agent", Live: "teleport"},
		{Component: "teleport", Field: "clusterAgent.chartVersion", Persisted: "18.1.0", Live: "18.2.0"},
	}, got)
}

func TestTeleport_RemovalIsReportedAsTheInstalledFlagOnly(t *testing.T) {
	got := Teleport(withTeleport(teleportInstalled()), withTeleport(state.TeleportState{}))

	require.Equal(t, []Change{
		{Component: "teleport", Field: "nodeAgent.installed", Persisted: "true", Live: "false"},
		{Component: "teleport", Field: "clusterAgent.installed", Persisted: "true", Live: "false"},
	}, got)
}

func TestTeleport_InstallIsReportedAsTheInstalledFlagOnly(t *testing.T) {
	got := Teleport(withTeleport(state.TeleportState{}), withTeleport(teleportInstalled()))

	require.Equal(t, []Change{
		{Component: "teleport", Field: "nodeAgent.installed", Persisted: "false", Live: "true"},
		{Component: "teleport", Field: "clusterAgent.installed", Persisted: "false", Live: "true"},
	}, got)
}

// The checker fills nodeAgent.version with the version this binary would
// install, so it differs after a provisioner upgrade with nothing changed.
func TestTeleport_IgnoresNodeAgentVersion(t *testing.T) {
	live := teleportInstalled()
	live.NodeAgent.Version = "18.9.9"

	require.Empty(t, Teleport(withTeleport(teleportInstalled()), withTeleport(live)))
}

// An unreachable cluster reads as an empty cluster agent, which must not be
// reported as the agent being removed.
func TestTeleport_SkipsClusterAgentWhenTheClusterWasNotReached(t *testing.T) {
	live := withTeleport(teleportInstalled())
	live.ClusterState.Created = false
	live.TeleportState.ClusterAgent = state.TeleportClusterAgentState{}

	require.Empty(t, Teleport(withTeleport(teleportInstalled()), live))
}

func TestTeleport_ClusterAgentDetailsAreNotComparedWhileUninstalled(t *testing.T) {
	persisted := state.TeleportState{ClusterAgent: state.TeleportClusterAgentState{Release: "stale"}}

	require.Empty(t, Teleport(withTeleport(persisted), withTeleport(state.TeleportState{})))
}

// teleportNotCompared lists the TeleportState keys the producer deliberately
// leaves out, with the reason.
var teleportNotCompared = map[string]string{
	"nodeAgent.version": "the checker reports the version this binary would install",
	"lastSync":          "refresh bookkeeping, not component state",
}

// Fails when a field is added to TeleportState without deciding whether drift
// detection compares it.
func TestTeleport_EveryStateFieldIsComparedOrExplicitlyExcluded(t *testing.T) {
	compared := map[string]bool{}
	for _, a := range []teleportAgent{teleportNodeAgent, teleportClusterAgent} {
		compared[a.installedField] = true
		for _, f := range a.fields {
			compared[f.name] = true
		}
	}

	for _, key := range yamlKeys(reflect.TypeFor[state.TeleportState](), "") {
		_, excluded := teleportNotCompared[key]
		require.Truef(t, compared[key] != excluded,
			"TeleportState key %q must be either compared by the Teleport producer or listed in teleportNotCompared", key)
	}
}

// yamlKeys returns the dotted yaml key of every leaf field of a struct type.
func yamlKeys(t reflect.Type, prefix string) []string {
	var keys []string
	for i := range t.NumField() {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if prefix != "" {
			name = prefix + "." + name
		}
		if f.Type.Kind() == reflect.Struct && f.Type.PkgPath() == t.PkgPath() {
			keys = append(keys, yamlKeys(f.Type, name)...)
			continue
		}
		keys = append(keys, name)
	}
	return keys
}
