// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package kube

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbeAPIServer_ReadyServer(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)
	useKubeconfig(t, server.URL)

	require.NoError(t, ProbeAPIServer(os.Getenv("KUBECONFIG"), 3*time.Second))
	assert.Equal(t, []string{"/readyz"}, paths)
}

func TestProbeAPIServer_UnreadyServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "[-]etcd failed", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	useKubeconfig(t, server.URL)

	require.Error(t, ProbeAPIServer(os.Getenv("KUBECONFIG"), 3*time.Second))
}

func TestProbeAPIServer_SilentServer(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	useKubeconfig(t, "http://"+addr)

	require.Error(t, ProbeAPIServer(os.Getenv("KUBECONFIG"), 3*time.Second))
}

// The probe must use only the file it is given: a default kubeconfig that
// points at a healthy server must not answer for a cluster that is down.
func TestProbeAPIServer_IgnoresDefaultKubeconfig(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(healthy.Close)
	useKubeconfig(t, healthy.URL)

	require.Error(t, ProbeAPIServer("/does/not/exist/admin.conf", 3*time.Second))
}
