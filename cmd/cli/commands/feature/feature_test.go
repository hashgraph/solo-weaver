// SPDX-License-Identifier: Apache-2.0

package feature

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/featuregate"
	"github.com/stretchr/testify/require"
)

func TestBuild_ResolvesStateAndMaturity(t *testing.T) {
	gate, err := featuregate.NewDefault()
	require.NoError(t, err)

	data := build(gate)
	require.Equal(t, uint(featuregate.CurrentActivationVersion), data.ActivationVersion)
	require.NotEmpty(t, data.Features)

	by := map[string]featureRow{}
	for _, r := range data.Features {
		by[r.Feature] = r
	}

	// GA feature is on at the floor; maturity is gatekit's canonical upper case.
	np := by[string(featuregate.NetworkPolicy)]
	require.Equal(t, "GA", np.Maturity)
	require.True(t, np.Enabled)

	// Alpha feature is listed, tagged ALPHA, and off by default.
	bn := by[string(featuregate.BlockNodeOperatorManaged)]
	require.Equal(t, "ALPHA", bn.Maturity)
	require.EqualValues(t, 2, bn.ActiveFrom)
	require.False(t, bn.Enabled)
	require.NotEmpty(t, bn.Description)
}

func TestRunList_Text(t *testing.T) {
	cmd := GetCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.RunE(cmd, nil))

	s := out.String()
	require.Contains(t, s, "FEATURE")
	require.Contains(t, s, string(featuregate.BlockNodeOperatorManaged))
	require.Contains(t, s, "--experimental")
}

func TestRunList_JSON(t *testing.T) {
	cmd := GetCmd()
	// --output is a root persistent flag; register it locally for the isolated cmd.
	cmd.Flags().String("output", "", "")
	require.NoError(t, cmd.Flags().Set("output", "json"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.RunE(cmd, nil))

	var got listing
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out.String())), &got))
	require.NotEmpty(t, got.Features)
}

func TestFilters_Apply(t *testing.T) {
	gate, err := featuregate.NewDefault()
	require.NoError(t, err)
	all := build(gate).Features

	// by activation version
	v2 := filters{activationVersion: 2, byActivation: true}.apply(all)
	require.NotEmpty(t, v2)
	for _, r := range v2 {
		require.EqualValues(t, 2, r.ActiveFrom)
	}

	// by maturity (upper-cased internally)
	ga := filters{maturity: "GA"}.apply(all)
	require.NotEmpty(t, ga)
	for _, r := range ga {
		require.Equal(t, "GA", r.Maturity)
	}

	// by enabled
	on := true
	enabled := filters{enabled: &on}.apply(all)
	require.NotEmpty(t, enabled)
	for _, r := range enabled {
		require.True(t, r.Enabled)
	}

	// Alpha + enabled is empty today (alpha is off by default).
	require.Empty(t, filters{maturity: "ALPHA", enabled: &on}.apply(all))
}

func TestList_RejectsUnknownMaturity(t *testing.T) {
	cmd := GetCmd()
	cmd.SetArgs([]string{"list", "--maturity", "bogus"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	require.Error(t, cmd.Execute())
}

// Regression for the sudo bug: the group and every subcommand must opt out of
// global checks, so `feature list` never runs the state-reading startup path.
func TestGetCmd_SkipsGlobalChecksOnSubtree(t *testing.T) {
	cmd := GetCmd()
	require.False(t, common.RequireGlobalChecks(cmd), "group must skip global checks")
	for _, sub := range cmd.Commands() {
		require.False(t, common.RequireGlobalChecks(sub), "subcommand %q must skip global checks", sub.Name())
	}
}
