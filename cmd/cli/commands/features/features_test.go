// SPDX-License-Identifier: Apache-2.0

package features

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

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

	// GA feature is on at the floor.
	np := by[string(featuregate.NetworkPolicy)]
	require.Equal(t, "ga", np.Maturity)
	require.True(t, np.Enabled)

	// Alpha feature is listed, tagged alpha, and off by default.
	bn := by[string(featuregate.BlockNodeOperatorManaged)]
	require.Equal(t, "alpha", bn.Maturity)
	require.EqualValues(t, 2, bn.ActivatesAt)
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
