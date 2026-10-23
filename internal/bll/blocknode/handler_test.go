// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package blocknode

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/security/principal"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
)

type funcChecker[T any] func() (T, error)

func (f funcChecker[T]) RefreshState(context.Context) (T, error) { return f() }

// newTestHandlers builds the real block-node handlers over a temp state file
// and checkers that report nothing live.
func newTestHandlers(t *testing.T) *Handlers {
	t.Helper()
	fm, err := fsx.NewManager(fsx.WithPrincipalManager(principal.NewMockManager(gomock.NewController(t))))
	require.NoError(t, err)
	sm, err := state.NewStateManager(state.WithState(state.NewState(filepath.Join(t.TempDir(), "state.yaml"))), state.WithFileManager(fm))
	require.NoError(t, err)
	checkers := reality.Checkers{
		Cluster:   funcChecker[state.ClusterState](func() (state.ClusterState, error) { return state.ClusterState{}, nil }),
		Machine:   funcChecker[state.MachineState](func() (state.MachineState, error) { return state.MachineState{}, nil }),
		BlockNode: funcChecker[state.BlockNodeState](func() (state.BlockNodeState, error) { return state.BlockNodeState{}, nil }),
		Consensus: funcChecker[map[string]state.ConsensusNodeState](func() (map[string]state.ConsensusNodeState, error) {
			return map[string]state.ConsensusNodeState{}, nil
		}),
		Teleport: funcChecker[state.TeleportState](func() (state.TeleportState, error) { return state.TeleportState{}, nil }),
	}
	runtime, err := rsl.NewRuntimeResolver(models.Config{}, sm, checkers, time.Minute)
	require.NoError(t, err)
	h, err := NewHandlerFactory(runtime)
	require.NoError(t, err)
	return h
}

// TestHandlerFactory_EachIntentOwnsOnlyTheFilesItWrites: an intent locks and
// flushes exactly the components it writes. Install owns the system record
// only when it will stand up Kubernetes, so on a host that already has it,
// install runs alongside a teleport node command.
func TestHandlerFactory_EachIntentOwnsOnlyTheFilesItWrites(t *testing.T) {
	h := newTestHandlers(t)
	orig := workflows.KubernetesInstalled
	t.Cleanup(func() { workflows.KubernetesInstalled = orig })

	all := map[string]bll.Component{
		"blocknode": bll.BlockNode, "profile": bll.Profile, "firewall": bll.Firewall, "system": bll.System,
		"cluster": bll.Cluster, "consensus": bll.ConsensusNode, "teleport": bll.Teleport,
	}
	for _, tt := range []struct {
		name                string
		action              models.ActionType
		kubernetesInstalled bool
		owns                []string
	}{
		{"install on a cluster", models.ActionInstall, true, []string{"blocknode", "profile", "firewall"}},
		{"install that creates the cluster", models.ActionInstall, false, []string{"blocknode", "profile", "firewall", "system"}},
		{"reconfigure", models.ActionReconfigure, true, []string{"blocknode", "firewall"}},
		{"upgrade", models.ActionUpgrade, true, []string{"blocknode"}},
		{"reset", models.ActionReset, true, []string{"blocknode"}},
		{"uninstall", models.ActionUninstall, true, []string{"blocknode"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			workflows.KubernetesInstalled = func() bool { return tt.kubernetesInstalled }
			handler, err := h.ForAction(tt.action)
			require.NoError(t, err)
			owner, ok := handler.(interface{ Owns(bll.Component) bool })
			require.True(t, ok, "%T must expose its BaseHandler", handler)

			for name, c := range all {
				require.Equalf(t, slices.Contains(tt.owns, name), owner.Owns(c), "owns %s", name)
			}
		})
	}
}

// TestStateCallbacks_PatchOnlyOwnedSections: a callback's changes reach disk
// only for the sections its base flushes, so each one must leave every other
// section alone. Install and reconfigure pass patchBlockNodeStateWithTrafficShaping
// and own blocknode and firewall; upgrade, reset and uninstall pass
// patchBlockNodeState and own blocknode alone.
func TestStateCallbacks_PatchOnlyOwnedSections(t *testing.T) {
	inputs := models.UserInputs[models.BlockNodeInputs]{Custom: models.BlockNodeInputs{
		Chart: "oci://example.com/block-node", PluginPreset: "full", TrafficShapingEnabled: true,
		EgressInterface: "eth0", LinkRate: "1gbit",
	}}

	for _, tt := range []struct {
		name         string
		callback     func(*state.State, models.UserInputs[models.BlockNodeInputs]) error
		ownsFirewall bool
	}{
		{"install, reconfigure", patchBlockNodeStateWithTrafficShaping(), true},
		{"upgrade, reset, uninstall", patchBlockNodeState(), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before, st := populatedState(), populatedState()

			require.NoError(t, tt.callback(&st, inputs))

			require.Equal(t, before.MachineState.Profile, st.MachineState.Profile)
			require.Equal(t, before.MachineState.Software, st.MachineState.Software)
			require.Equal(t, before.MachineState.Hardware, st.MachineState.Hardware)
			require.Equal(t, before.ClusterState, st.ClusterState)
			require.Equal(t, before.ConsensusNodes, st.ConsensusNodes)
			require.Equal(t, before.TeleportState, st.TeleportState)
			if !tt.ownsFirewall {
				require.Equal(t, before.MachineState.Firewall, st.MachineState.Firewall)
			}
		})
	}
}

// populatedState returns a State with every section set, so a callback that
// touched one would show.
func populatedState() state.State {
	s := state.NewState("")
	s.MachineState.Profile = "mainnet"
	s.MachineState.Software["crio"] = state.SoftwareState{Name: "cri-o", Version: "1.30.0", Installed: true}
	s.MachineState.Hardware["memory"] = state.HardwareState{Type: "memory", Info: "8 GB available"}
	s.MachineState.Firewall = &state.HostFirewallState{ManagementCIDRs: []string{"10.0.0.0/8"}}
	s.ClusterState.Created = true
	s.BlockNodeState.ReleaseInfo.Name = "block-node"
	s.BlockNodeState.ReleaseInfo.Status = release.StatusDeployed
	s.ConsensusNodes = map[string]state.ConsensusNodeState{"node1": {NodeId: 1}}
	s.TeleportState.NodeAgent = state.TeleportNodeAgentState{Installed: true}
	return s
}
