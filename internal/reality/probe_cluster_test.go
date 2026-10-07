// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package reality

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/security/principal"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
)

func TestProbeCluster_NoKubeconfigIsNotConfigured(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "no-such-kubeconfig"))

	require.Equal(t, NotConfigured, probeCluster())
}

// useKubeconfig points the probe at a kubeconfig whose only cluster is server.
func useKubeconfig(t *testing.T, server string) {
	t.Helper()
	kubeconfig := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(kubeconfig, fmt.Appendf(nil, `apiVersion: v1
kind: Config
clusters: [{name: c, cluster: {server: "%s"}}]
contexts: [{name: c, context: {cluster: c, user: u}}]
current-context: c
users: [{name: u, user: {}}]
`, server), 0o600))
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBECONFIG", kubeconfig)
}

func TestProbeCluster_SilentAPIServerIsUnobservable(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	useKubeconfig(t, "http://"+addr)

	require.Equal(t, Unobservable, probeCluster())
}

func TestProbeCluster_AnsweringAPIServerIsReachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"major":"1","minor":"33","gitVersion":"v1.33.0"}`))
	}))
	t.Cleanup(server.Close)
	useKubeconfig(t, server.URL)

	require.Equal(t, Reachable, probeCluster())
}

type noHelmReleases struct{}

func (noHelmReleases) ListAll() ([]*release.Release, error) { return nil, nil }

// Unobservable and NotConfigured both mean the cluster was not observed, so the
// cluster, block node and consensus checkers refresh the same way for each.
func TestCheckers_UnobservableIsHandledLikeNotConfigured(t *testing.T) {
	fm, err := fsx.NewManager(fsx.WithPrincipalManager(principal.NewMockManager(gomock.NewController(t))))
	require.NoError(t, err)
	persisted := state.NewState(filepath.Join(t.TempDir(), "state.yaml"))
	persisted.BlockNodeState.ReleaseInfo.Name = "block-node"
	persisted.ConsensusNodes = map[string]state.ConsensusNodeState{"node1": {NodeId: 1}}
	sm, err := state.NewStateManager(state.WithState(persisted), state.WithFileManager(fm))
	require.NoError(t, err)

	notConfigured := func() ClusterReachability { return NotConfigured }
	unobservable := func() ClusterReachability { return Unobservable }
	newHelm := func() (HelmManager, error) { return noHelmReleases{}, nil }
	newKube := func() (KubeClient, error) { return nil, errors.New("not called") }
	newConsensusKube := func() (ConsensusKubeClient, error) { return nil, errors.New("not called") }

	refresh := func(probe ClusterProbe) (state.ClusterState, state.BlockNodeState, map[string]state.ConsensusNodeState) {
		cluster, err := NewClusterChecker(sm, probe)
		require.NoError(t, err)
		blockNode, err := NewBlockNodeChecker(sm, newHelm, newKube, probe)
		require.NoError(t, err)
		consensus, err := NewConsensusChecker(sm, newConsensusKube, probe)
		require.NoError(t, err)

		cs, err := cluster.RefreshState(context.Background())
		require.NoError(t, err)
		bn, err := blockNode.RefreshState(context.Background())
		require.NoError(t, err)
		cn, err := consensus.RefreshState(context.Background())
		require.NoError(t, err)
		return cs, bn, cn
	}

	wantCS, wantBN, wantCN := refresh(notConfigured)
	gotCS, gotBN, gotCN := refresh(unobservable)

	require.Equal(t, wantCS, gotCS)
	require.Equal(t, wantBN, gotBN)
	require.Equal(t, wantCN, gotCN)
	require.Equal(t, "block-node", gotBN.ReleaseInfo.Name)
	require.Contains(t, gotCN, "node1")
}
