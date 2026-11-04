// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package consensus

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
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/security/principal"
	"github.com/stretchr/testify/require"
)

type funcChecker[T any] func() (T, error)

func (f funcChecker[T]) RefreshState(context.Context) (T, error) { return f() }

// TestHandlerFactory_EachIntentOwnsOnlyTheFilesItWrites: an intent locks and
// flushes exactly the components it writes. Consensus has only install, which
// records the profile. A new intent fails here until it gets its own row: one
// that reused the install base would also lock and rewrite profile.yaml.
func TestHandlerFactory_EachIntentOwnsOnlyTheFilesItWrites(t *testing.T) {
	h := newTestHandlers(t)

	all := map[string]bll.Component{
		"blocknode": bll.BlockNode, "profile": bll.Profile, "firewall": bll.Firewall, "system": bll.System,
		"cluster": bll.Cluster, "consensus": bll.ConsensusNode, "teleport": bll.Teleport,
	}
	owns := map[models.ActionType][]string{
		models.ActionInstall: {"consensus", "profile"},
	}

	for _, action := range []models.ActionType{
		models.ActionSetup, models.ActionInstall, models.ActionUninstall, models.ActionReset, models.ActionDeploy,
		models.ActionDestroy, models.ActionUpgrade, models.ActionMigrate, models.ActionReconfigure,
	} {
		t.Run(string(action), func(t *testing.T) {
			handler, err := h.ForAction(action)
			want, supported := owns[action]
			if !supported {
				require.Error(t, err, "consensus now handles %q: add its row to owns", action)
				return
			}
			require.NoError(t, err)
			owner, ok := handler.(interface{ Owns(bll.Component) bool })
			require.True(t, ok, "%T must expose its BaseHandler", handler)
			for name, c := range all {
				require.Equalf(t, slices.Contains(want, name), owner.Owns(c), "owns %s", name)
			}
		})
	}
}

// newTestHandlers builds the real consensus handlers over a temp state file
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
