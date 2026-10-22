// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package node

import (
	"testing"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateUninstallFlags(t *testing.T) {
	tests := []struct {
		name      string
		nodeIdSet bool
		namespace string
		nodeId    int64
		wantErr   string
	}{
		{"node 0 is valid", true, "hiero-network-1", 0, ""},
		{"node-id missing", false, "hiero-network-1", 0, "--node-id is required"},
		{"negative node-id", true, "hiero-network-1", -1, "must not be negative"},
		{"empty namespace", true, "  ", 0, "--namespace is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateUninstallFlags(tt.nodeIdSet, tt.namespace, tt.nodeId)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			reason, ok := errx.ReasonOf(err)
			require.True(t, ok, "error must carry a reason code")
			assert.Equal(t, reasons.InvalidArgument, reason)
		})
	}
}

// A non-empty profile would overwrite the machine profile in state on flush.
func TestConsensusUninstallInputs_NoProfileAndValid(t *testing.T) {
	origNs, origId, origProfile := flagNamespace, flagNodeId, flagProfile
	t.Cleanup(func() { flagNamespace, flagNodeId, flagProfile = origNs, origId, origProfile })
	flagNamespace, flagNodeId, flagProfile = "hiero-network-1", 3, "mainnet"

	inputs := consensusUninstallInputs(true)

	assert.Empty(t, inputs.Custom.Profile)
	assert.Equal(t, "hiero-network-1", inputs.Custom.Namespace)
	assert.Equal(t, int64(3), inputs.Custom.NodeId)
	assert.True(t, inputs.Common.Force)
	require.NoError(t, inputs.Validate(), "the base handler validates these inputs")
}

func TestUninstallCmd_IsRegistered(t *testing.T) {
	found := false
	for _, c := range GetCmd().Commands() {
		if c.Name() == "uninstall" {
			found = true
		}
	}
	assert.True(t, found, "consensus node must have an uninstall subcommand")
}
