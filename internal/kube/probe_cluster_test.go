// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package kube

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// useKubeconfig points the probe at a kubeconfig whose only cluster is server.
func useKubeconfig(t *testing.T, server string) {
	t.Helper()
	kubeconfig := filepath.Join(t.TempDir(), "config")
	content := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: c
  cluster:
    server: %s
contexts:
- name: c
  context:
    cluster: c
    user: u
current-context: c
users:
- name: u
  user: {}
`, server)
	require.NoError(t, os.WriteFile(kubeconfig, []byte(content), 0o600))
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBECONFIG", kubeconfig)
}

func TestProbeCluster_NoKubeconfigIsNotConfigured(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "no-such-kubeconfig"))

	configured, reachable := ProbeCluster()

	require.False(t, configured)
	require.False(t, reachable)
}

func TestProbeCluster_UnparseableKubeconfigIsConfiguredButUnreachable(t *testing.T) {
	kubeconfig := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(kubeconfig, []byte{}, 0o600))
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBECONFIG", kubeconfig)

	configured, reachable := ProbeCluster()

	require.True(t, configured)
	require.False(t, reachable)
}

func TestProbeCluster_SilentAPIServerIsConfiguredButUnreachable(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	useKubeconfig(t, "http://"+addr)

	configured, reachable := ProbeCluster()

	require.True(t, configured)
	require.False(t, reachable)
	exists, err := ClusterExists()
	require.NoError(t, err)
	require.False(t, exists)
}

func TestProbeCluster_AnsweringAPIServerIsReachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"major":"1","minor":"33","gitVersion":"v1.33.0"}`))
	}))
	t.Cleanup(server.Close)
	useKubeconfig(t, server.URL)

	configured, reachable := ProbeCluster()

	require.True(t, configured)
	require.True(t, reachable)
	exists, err := ClusterExists()
	require.NoError(t, err)
	require.True(t, exists)
}
