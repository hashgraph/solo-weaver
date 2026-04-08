// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"context"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/deps"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/joomcode/errorx"
	htime "helm.sh/helm/v3/pkg/time"
)

// teleportChecker probes the Teleport node agent (binary) and cluster agent (Helm)
// to build a TeleportState.
type teleportChecker struct {
	Base
	newHelm       func() (HelmManager, error)
	clusterExists ClusterProbe
}

// NewTeleportChecker constructs a teleportChecker.
func NewTeleportChecker(
	sm state.Manager,
	newHelm func() (HelmManager, error),
	clusterExists ClusterProbe,
) (Checker[state.TeleportState], error) {
	return &teleportChecker{
		Base:          Base{sm: sm},
		newHelm:       newHelm,
		clusterExists: clusterExists,
	}, nil
}

func (t *teleportChecker) FlushState(st state.TeleportState) error {
	if err := t.sm.Refresh(); err != nil && !errorx.IsOfType(err, state.NotFoundError) {
		return ErrFlushError.Wrap(err, "failed to refresh state")
	}

	fullState := t.sm.State()
	fullState.TeleportState = st
	if err := t.sm.Set(fullState).FlushState(); err != nil {
		return ErrFlushError.Wrap(err, "failed to persist state with refreshed TeleportState")
	}

	return nil
}

func (t *teleportChecker) RefreshState(ctx context.Context) (state.TeleportState, error) {
	now := htime.Now()
	ts := t.sm.State().TeleportState

	// Refresh node agent state via live binary verification
	nodeState, err := t.refreshNodeAgentState()
	if err != nil {
		logx.As().Warn().Err(err).Msg("Failed to refresh teleport node agent state")
	} else {
		ts.NodeAgent = nodeState
	}

	// Refresh cluster agent state via Helm
	clusterState, err := t.refreshClusterAgentState()
	if err != nil {
		logx.As().Warn().Err(err).Msg("Failed to refresh teleport cluster agent state")
	} else {
		ts.ClusterAgent = clusterState
	}

	ts.LastSync = now
	return ts, nil
}

func (t *teleportChecker) refreshNodeAgentState() (state.TeleportNodeAgentState, error) {
	installer, err := software.NewTeleportNodeAgentInstaller(software.WithStateManager(t.sm))
	if err != nil {
		return state.TeleportNodeAgentState{}, err
	}

	st, err := installer.VerifyInstallation()
	if err != nil {
		return state.TeleportNodeAgentState{}, err
	}

	return state.TeleportNodeAgentState{
		Installed:  st.Installed,
		Configured: st.Configured,
		Version:    st.Version,
	}, nil
}

func (t *teleportChecker) refreshClusterAgentState() (state.TeleportClusterAgentState, error) {
	exists, err := t.clusterExists()
	if !exists {
		logx.As().Debug().Err(err).Msg("Kubernetes cluster does not exist, skipping teleport cluster agent check")
		return state.TeleportClusterAgentState{}, nil
	}

	hm, err := t.newHelm()
	if err != nil {
		return state.TeleportClusterAgentState{}, errorx.IllegalState.Wrap(err, "failed to create helm manager")
	}

	releases, err := hm.ListAll()
	if err != nil {
		return state.TeleportClusterAgentState{}, errorx.IllegalState.Wrap(err, "failed to list helm releases")
	}

	for _, rel := range releases {
		if rel.Name == deps.TELEPORT_RELEASE && rel.Namespace == deps.TELEPORT_NAMESPACE {
			return state.TeleportClusterAgentState{
				Installed:    true,
				Release:      rel.Name,
				Namespace:    rel.Namespace,
				ChartVersion: rel.Chart.Metadata.Version,
			}, nil
		}
	}

	return state.TeleportClusterAgentState{}, nil
}
