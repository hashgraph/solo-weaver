// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package drift

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/stretchr/testify/require"
)

func fixed(changes ...Change) Producer {
	return func(_, _ state.State) []Change { return changes }
}

func TestDetect_NoChangesIsEmpty(t *testing.T) {
	require.Empty(t, Detect(state.State{}, state.State{}))
	require.Empty(t, Detect(state.State{}, state.State{}, fixed(), fixed()))
}

func TestDetect_KeepsEveryChangeInProducerOrder(t *testing.T) {
	a := Change{Component: "a", Field: "x", Persisted: "1", Live: "2"}
	b1 := Change{Component: "b", Field: "y", Persisted: "3", Live: "4"}
	b2 := Change{Component: "b", Field: "z", Persisted: "", Live: "5"}

	got := Detect(state.State{}, state.State{}, fixed(a), fixed(), fixed(b1, b2))

	require.Equal(t, []Change{a, b1, b2}, got)
}

func TestDetect_GivesEveryProducerTheSameBaselineAndLiveState(t *testing.T) {
	baseline := state.State{StateFile: "baseline"}
	live := state.State{StateFile: "live"}
	var seen []string
	record := func(b, l state.State) []Change {
		seen = append(seen, b.StateFile+"/"+l.StateFile)
		return nil
	}

	Detect(baseline, live, record, record)

	require.Equal(t, []string{"baseline/live", "baseline/live"}, seen)
}

func TestChange_StringNamesComponentFieldAndBothValues(t *testing.T) {
	c := Change{Component: "teleport", Field: "clusterAgent.chartVersion", Persisted: "18.1.0", Live: ""}

	require.Equal(t,
		`teleport clusterAgent.chartVersion differs from persisted state: state.yaml has "18.1.0", live is ""`,
		c.String())
}
