// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package state

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	htime "helm.sh/helm/v3/pkg/time"
)

func teleportStateFixture() TeleportState {
	return TeleportState{
		NodeAgent: TeleportNodeAgentState{
			Installed:  true,
			Configured: true,
			Version:    "18.6.4",
		},
		ClusterAgent: TeleportClusterAgentState{
			Installed:    true,
			Release:      "teleport-cluster-agent",
			Namespace:    "teleport-agent",
			ChartVersion: "18.6.4",
		},
	}
}

func TestTeleportState_Equal(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*TeleportState)
		want   bool
	}{
		{name: "identical", mutate: func(*TeleportState) {}, want: true},
		{name: "lastSync ignored", mutate: func(s *TeleportState) { s.LastSync = htime.Now() }, want: true},
		{name: "nodeAgent.installed", mutate: func(s *TeleportState) { s.NodeAgent.Installed = false }, want: false},
		{name: "nodeAgent.configured", mutate: func(s *TeleportState) { s.NodeAgent.Configured = false }, want: false},
		{name: "nodeAgent.version", mutate: func(s *TeleportState) { s.NodeAgent.Version = "18.7.0" }, want: false},
		{name: "clusterAgent.installed", mutate: func(s *TeleportState) { s.ClusterAgent.Installed = false }, want: false},
		{name: "clusterAgent.release", mutate: func(s *TeleportState) { s.ClusterAgent.Release = "other" }, want: false},
		{name: "clusterAgent.namespace", mutate: func(s *TeleportState) { s.ClusterAgent.Namespace = "other" }, want: false},
		{name: "clusterAgent.chartVersion", mutate: func(s *TeleportState) { s.ClusterAgent.ChartVersion = "18.7.0" }, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := teleportStateFixture()
			other := teleportStateFixture()
			tt.mutate(&other)
			assert.Equal(t, tt.want, base.Equal(other))
			assert.Equal(t, tt.want, other.Equal(base))
		})
	}
}

// Fails when a field is added to a Teleport state struct without Equal comparing it.
func TestTeleportState_Equal_CoversAllFields(t *testing.T) {
	for _, path := range leafFieldPaths(t, reflect.TypeOf(TeleportState{}), nil) {
		t.Run(fieldPathName(reflect.TypeOf(TeleportState{}), path), func(t *testing.T) {
			var other TeleportState
			setNonZero(t, reflect.ValueOf(&other).Elem().FieldByIndex(path))
			assert.False(t, (&TeleportState{}).Equal(other), "Equal ignores this field")
		})
	}
}

func leafFieldPaths(t *testing.T, typ reflect.Type, prefix []int) [][]int {
	t.Helper()
	var paths [][]int
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Name == "LastSync" {
			continue
		}
		path := append(append([]int{}, prefix...), i)
		if f.Type.Kind() == reflect.Struct {
			paths = append(paths, leafFieldPaths(t, f.Type, path)...)
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

func fieldPathName(typ reflect.Type, path []int) string {
	name := ""
	for _, i := range path {
		f := typ.Field(i)
		if name != "" {
			name += "."
		}
		name += f.Name
		typ = f.Type
	}
	return name
}

func setNonZero(t *testing.T, v reflect.Value) {
	t.Helper()
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(true)
	case reflect.String:
		v.SetString("x")
	default:
		require.FailNowf(t, "unsupported field kind", "extend setNonZero to handle %s", v.Kind())
	}
}
