// SPDX-License-Identifier: Apache-2.0

package state

import (
	"os"
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
)

// TestReadPromptDefaultsFromDisk_FilesDoNotClobberEachOther covers the case
// where profile.yaml, firewall.yaml and blocknode.yaml all exist. Each is a
// marshaled State, so each carries the others' sections zero-valued. Decoding
// the files into one shared doc would let a later decode silently overwrite
// the Profile or Firewall an earlier one just loaded.
func TestReadPromptDefaultsFromDisk_FilesDoNotClobberEachOther(t *testing.T) {
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
	require.NoError(t, sm.Commit(ActionHistory{}, AllComponentIDs...))

	got, err := ReadPromptDefaultsFromDisk()
	require.NoError(t, err)

	require.Equal(t, "mainnet", got.Profile, "profile must survive the other files also being present")
	require.NotNil(t, got.Firewall, "firewall must survive the other files also being present")
	require.Equal(t, []string{"10.0.0.0/8"}, got.Firewall.ManagementCIDRs)
	require.Equal(t, "my-release", got.BlockNode.ReleaseName)
	require.Equal(t, "my-ns", got.BlockNode.Namespace)
}
