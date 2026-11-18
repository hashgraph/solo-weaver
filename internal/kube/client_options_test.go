// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package kube

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// kubeconfigFile writes a kubeconfig for server and returns its path, leaving
// $KUBECONFIG alone.
func kubeconfigFile(t *testing.T, server string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "admin.conf")
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
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// A hung API server (up, but with etcd down) answers nothing; with a request
// timeout the client gives up instead of blocking the command.
func TestNewClient_RequestTimeoutBoundsAHungServer(t *testing.T) {
	release := make(chan struct{})
	hung := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(hung.Close)
	t.Cleanup(func() { close(release) })

	start := time.Now()
	kc, err := NewClient(WithKubeconfig(kubeconfigFile(t, hung.URL)), WithRequestTimeout(200*time.Millisecond))
	require.NoError(t, err)
	_, err = kc.ResourceExists(context.Background(), "v1", "ConfigMap", "kube-system", "cilium-config")

	require.Error(t, err)
	require.Less(t, time.Since(start), 5*time.Second)
}

// The client must use only the file it is given: a default kubeconfig that
// points at a healthy server must not answer for the admin one.
func TestNewClient_WithKubeconfigIgnoresTheDefault(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"kind":"APIVersions","versions":["v1"]}`))
	}))
	t.Cleanup(healthy.Close)
	useKubeconfig(t, healthy.URL)

	_, err := NewClient(WithKubeconfig("/does/not/exist/admin.conf"))

	require.Error(t, err)
}
