// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/provenance"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// fakeCollector returns a fixed Input, so the gate's verdict is controlled by the
// test rather than by the machine.
type fakeCollector struct{ in provenance.Input }

func (f fakeCollector) Collect(context.Context, provenance.Need, bool) (provenance.Input, error) {
	return f.in, nil
}

func withCollector(t *testing.T, in provenance.Input) {
	t.Helper()
	prev := provenanceCollector
	provenanceCollector = fakeCollector{in: in}
	t.Cleanup(func() { provenanceCollector = prev })
}

func TestProvenanceNeed_DefaultsAndInheritance(t *testing.T) {
	// Unset => NeedNone.
	leaf := &cobra.Command{Use: "leaf"}
	require.Equal(t, provenance.NeedNone, ProvenanceNeed(leaf))

	// Declared on the command itself.
	SetProvenanceNeed(leaf, provenance.NeedHostAPI)
	require.Equal(t, provenance.NeedHostAPI, ProvenanceNeed(leaf))

	// Declared on a parent is inherited by a child that did not declare.
	parent := &cobra.Command{Use: "parent"}
	child := &cobra.Command{Use: "child"}
	parent.AddCommand(child)
	SetProvenanceNeed(parent, provenance.NeedHost)
	require.Equal(t, provenance.NeedHost, ProvenanceNeed(child))
}

func TestRunProvenanceGate_ProvisionedPasses(t *testing.T) {
	withCollector(t, provenance.Input{
		RecordedMachineIDPresent: true, MachineIDMatches: true,
		HasProvisionRecord: true, TargetMatchesProvisionRecord: true,
		LocalEvidencePasses: true, APIReachable: true,
		LiveClusterID: "u1", ProvisionedClusterID: "u1",
	})
	cmd := &cobra.Command{Use: "install"}
	origin, err := RunProvenanceGate(cmd, provenance.NeedHostAPI)
	require.NoError(t, err)
	require.Equal(t, provenance.Provisioned, origin)
}

func TestRunProvenanceGate_StopSurfacesError(t *testing.T) {
	// Rebuilt cluster (A.4) must stop a host+api command.
	withCollector(t, provenance.Input{
		RecordedMachineIDPresent: true, MachineIDMatches: true,
		HasProvisionRecord: true, TargetMatchesProvisionRecord: true,
		LocalEvidencePasses: true, APIReachable: true,
		LiveClusterID: "u-new", ProvisionedClusterID: "u-old",
	})
	cmd := &cobra.Command{Use: "install"}
	_, err := RunProvenanceGate(cmd, provenance.NeedHostAPI)
	require.Error(t, err)
}

func TestRunProvenanceGate_AllowlistNarrows(t *testing.T) {
	// A valid external target (api command) is allowed with no restriction...
	withCollector(t, provenance.Input{
		HasProvisionRecord: false, LocalEvidencePasses: false,
		KubeconfigGiven: true, APIReachable: true, LiveClusterID: "u-ext",
	})
	cmd := &cobra.Command{Use: "upgrade"}
	origin, err := RunProvenanceGate(cmd, provenance.NeedAPI)
	require.NoError(t, err)
	require.Equal(t, provenance.External, origin)

	// ...but refused when the host allowlist permits provisioned only.
	t.Setenv(allowedOriginsEnv, "provisioned")
	_, err = RunProvenanceGate(cmd, provenance.NeedAPI)
	require.Error(t, err)
}
