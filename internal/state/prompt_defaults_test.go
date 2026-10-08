// SPDX-License-Identifier: Apache-2.0

package state

import (
	"os"
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
)

// TestReadPromptDefaultsFromDisk_BlockNodeFileDoesNotClobberMachineFields
// covers the case where both machine.yaml and blocknode.yaml exist.
// blocknode.yaml is a marshaled State too, so it carries a zero-valued
// machineState section (MachineState has no omitempty tag). Decoding both
// files into one shared doc would let the second decode (blocknode) silently
// overwrite the Profile/Firewall the first decode (machine) just loaded.
func TestReadPromptDefaultsFromDisk_BlockNodeFileDoesNotClobberMachineFields(t *testing.T) {
	home := t.TempDir()
	t.Cleanup(models.SetPaths(home))
	require.NoError(t, os.MkdirAll(models.Paths().StateDir, 0o755))

	s := NewState("")
	s.MachineState.Profile = "mainnet"
	s.MachineState.Firewall = &HostFirewallState{ManagementCIDRs: []string{"10.0.0.0/8"}}
	s.BlockNodeState.ReleaseInfo.Name = "my-release"
	s.BlockNodeState.ReleaseInfo.Namespace = "my-ns"

	sm, err := NewStateManager(WithState(s), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)
	require.NoError(t, sm.FlushState())

	got, err := ReadPromptDefaultsFromDisk()
	require.NoError(t, err)

	require.Equal(t, "mainnet", got.Profile, "profile must survive blocknode.yaml also being present")
	require.NotNil(t, got.Firewall, "firewall must survive blocknode.yaml also being present")
	require.Equal(t, []string{"10.0.0.0/8"}, got.Firewall.ManagementCIDRs)
	require.Equal(t, "my-release", got.BlockNode.ReleaseName)
	require.Equal(t, "my-ns", got.BlockNode.Namespace)
}
